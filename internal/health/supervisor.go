package health

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/util"
)

// entry is one supervised component.
type entry struct {
	name      string
	factory   Factory
	grace     time.Duration // healthy grace: 5 × longest device interval
	comp      Component
	restarts  int
	restarted time.Time
	state     State     // seen at the last sweep (snapshot never calls components)
	beat      time.Time // seen at the last sweep
}

// Supervisor owns all restartable components.
type Supervisor struct {
	clock  util.Clock
	log    zerolog.Logger
	sweep  time.Duration
	cancel context.CancelCauseFunc

	// op serialises the sweep with Set and Remove, so a component is never restarted
	// and replaced at once.
	op  sync.Mutex
	ctx context.Context // the Run context; nil before Run

	mu      sync.Mutex
	entries []*entry
	devices map[string]string
	polls   map[string]Poll
	swept   time.Time
	fatal   error
}

// NewSupervisor builds a supervisor; cancel is the root context's cancel function.
func NewSupervisor(clock util.Clock, sweep time.Duration, cancel context.CancelCauseFunc,
	log zerolog.Logger,
) (*Supervisor, error) {
	err := util.RequireAll("health", util.D("clock", clock), util.D("cancel", cancel))
	if err != nil {
		return nil, err
	}
	return &Supervisor{
		clock: clock, sweep: sweep, cancel: cancel, devices: map[string]string{},
		polls: map[string]Poll{},
		log:   log.With().Str("component", "health").Logger(),
	}, nil
}

// Add registers a component; it is built and started by Run.
func (s *Supervisor) Add(name string, f Factory, grace time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, &entry{name: name, factory: f, grace: grace})
}

// DevicePoll implements Reporter.
func (s *Supervisor) DevicePoll(device string, poll Poll) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls[device] = poll
}

// DeviceState implements Reporter.
func (s *Supervisor) DeviceState(device, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[device] = state
}

// Set registers a component or replaces the one with the same name (configuration
// change, spec 3): the old instance is stopped, the new one is built and started at once
// when Run is active, with a fresh restart budget.
func (s *Supervisor) Set(name string, f Factory, grace time.Duration) {
	s.op.Lock()
	defer s.op.Unlock()
	e := &entry{name: name, factory: f, grace: grace}
	s.mu.Lock()
	old := s.take(name)
	s.entries = append(s.entries, e)
	ctx := s.ctx
	s.mu.Unlock()
	if old != nil && old.comp != nil {
		if err := s.stop(old.comp); err != nil {
			s.failFatal(fmt.Errorf("%s: %w", name, err))
			return
		}
	}
	if ctx == nil {
		return
	}
	if err := s.start(ctx, e); err != nil {
		s.log.Error().Err(err).Str("name", name).Msg("start failed")
	}
}

// Remove stops and removes a component; unknown names are ignored.
func (s *Supervisor) Remove(name string) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	old := s.take(name)
	s.mu.Unlock()
	if old == nil || old.comp == nil {
		return
	}
	if err := s.stop(old.comp); err != nil {
		s.failFatal(fmt.Errorf("%s: %w", name, err))
	}
}

// Names returns the names of all components.
func (s *Supervisor) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.name
	}
	return out
}

// take removes the entry name and returns it; the caller holds mu.
func (s *Supervisor) take(name string) *entry {
	for i, e := range s.entries {
		if e.name == name {
			s.entries = append(s.entries[:i:i], s.entries[i+1:]...)
			return e
		}
	}
	return nil
}

// ForgetDevice drops the state and last poll of a device that left the configuration.
func (s *Supervisor) ForgetDevice(device string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices, device)
	delete(s.polls, device)
}

// Run starts every component and sweeps until ctx ends, then stops them all.
func (s *Supervisor) Run(ctx context.Context) error {
	s.op.Lock()
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	for _, e := range s.snapshotEntries() {
		if err := s.start(ctx, e); err != nil {
			s.failFatal(fmt.Errorf("start %s: %w", e.name, err))
		}
	}
	s.op.Unlock()
	t := s.clock.NewTicker(s.sweep)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return s.stopAll()
		case <-t.C():
			s.check(ctx)
		}
	}
}

func (s *Supervisor) snapshotEntries() []*entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*entry(nil), s.entries...)
}

func (s *Supervisor) start(ctx context.Context, e *entry) error {
	c, err := e.factory()
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	s.mu.Lock()
	e.comp, e.state, e.beat = c, Healthy, s.clock.Now()
	s.mu.Unlock()
	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

// check runs one sweep: restart failed or silent components, reset budgets.
func (s *Supervisor) check(ctx context.Context) {
	s.op.Lock()
	defer s.op.Unlock()
	now := s.clock.Now()
	for _, e := range s.snapshotEntries() {
		state, beat := Failed, time.Time{}
		if e.comp != nil {
			state, beat = e.comp.State(), e.comp.LastBeat()
		}
		s.mu.Lock()
		e.state, e.beat = state, beat
		s.mu.Unlock()
		if state != Failed && now.Sub(beat) <= e.grace {
			s.mu.Lock()
			if e.restarts > 0 && now.Sub(e.restarted) >= e.grace {
				e.restarts = 0 // the restarted instance kept beating for the grace
			}
			s.mu.Unlock()
			continue
		}
		s.restart(ctx, e, now)
	}
	s.mu.Lock()
	s.swept = now
	s.mu.Unlock()
}

func (s *Supervisor) restart(ctx context.Context, e *entry, now time.Time) {
	if e.restarts >= RestartBudget {
		s.failFatal(fmt.Errorf("%s: restart budget exhausted", e.name))
		return
	}
	s.log.Warn().Str("name", e.name).Int("restart", e.restarts+1).Msg("restarting component")
	if e.comp != nil {
		if err := s.stop(e.comp); err != nil {
			s.failFatal(fmt.Errorf("%s: %w", e.name, err))
			return
		}
	}
	s.mu.Lock()
	e.restarts++
	e.restarted = now
	s.mu.Unlock()
	if err := s.start(ctx, e); err != nil {
		s.log.Error().Err(err).Str("name", e.name).Msg("restart failed")
	}
}

// stop runs Stop with the injected clock as timeout.
func (s *Supervisor) stop(c Component) error {
	done := make(chan error, 1)
	go func() { done <- c.Stop() }()
	t := s.clock.NewTimer(StopTimeout)
	defer t.Stop()
	select {
	case err := <-done:
		if err != nil {
			s.log.Warn().Err(err).Msg("stop failed")
		}
		return nil
	case <-t.C():
		return fmt.Errorf("stop exceeded %s", StopTimeout)
	}
}

func (s *Supervisor) stopAll() error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	s.ctx = nil
	s.mu.Unlock()
	var firstErr error
	for _, e := range s.snapshotEntries() {
		if e.comp == nil {
			continue
		}
		if err := s.stop(e.comp); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("health: %s: %w", e.name, err)
		}
	}
	return firstErr
}

// Fail reports a failure of a non-restartable part (storage, event bus, HTTP server).
func (s *Supervisor) Fail(err error) { s.failFatal(err) }

func (s *Supervisor) failFatal(err error) {
	s.mu.Lock()
	if s.fatal == nil {
		s.fatal = fmt.Errorf("%w: %w", ErrHealthFatal, err)
	}
	fatal := s.fatal
	s.mu.Unlock()
	s.log.Error().Err(err).Msg("fatal health error")
	s.cancel(fatal)
}

// ComponentStatus is one row of the snapshot.
type ComponentStatus struct {
	Name     string    `json:"name"`
	State    string    `json:"state"`
	Restarts int       `json:"restarts"`
	LastBeat time.Time `json:"last_beat"`
}

// Snapshot is the health view for GET /health.
type Snapshot struct {
	At         time.Time // time of the last sweep
	Components []ComponentStatus
	Devices    map[string]string
	Polls      map[string]Poll
	Fatal      bool
}

// Snapshot returns the current state without blocking on components.
func (s *Supervisor) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{
		At: s.swept, Devices: maps.Clone(s.devices), Polls: maps.Clone(s.polls),
		Fatal: s.fatal != nil,
	}
	for _, e := range s.entries {
		state := e.state
		if e.comp == nil {
			state = Failed
		}
		snap.Components = append(snap.Components, ComponentStatus{
			Name: e.name, State: state.String(), Restarts: e.restarts, LastBeat: e.beat,
		})
	}
	sort.Slice(snap.Components, func(i, j int) bool {
		return snap.Components[i].Name < snap.Components[j].Name
	})
	return snap
}
