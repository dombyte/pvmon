package modbus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	gridx "github.com/grid-x/modbus"
	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/util"
)

// Conn is the shared connection of one address. Requests from all devices on it go through
// one FIFO queue served by a single worker goroutine, which alone owns the Client.
type Conn struct {
	settings Settings
	clock    util.Clock
	opener   Opener
	log      zerolog.Logger

	queue chan *request // never closed; worker exits on done
	done  chan struct{}
	wg    sync.WaitGroup
	once  sync.Once
	start sync.Once

	// owned by the worker goroutine
	client  Client
	backoff util.Backoff
}

type request struct {
	ctx   context.Context
	unit  uint8
	fn    Function
	start uint16
	count uint16
	reply chan result // buffered 1; written once by the worker
}

type result struct {
	words []uint16
	err   error
}

// queueSize bounds pending requests; a full queue blocks the caller until its ctx ends.
const (
	queueSize        = 16
	bytesPerRegister = 2
)

// New builds a connection; it never dials, so an unreachable device does not fail startup.
func New(s Settings, clock util.Clock, opener Opener, log zerolog.Logger) (*Conn, error) {
	err := util.RequireAll("modbus", util.D("clock", clock), util.D("opener", opener))
	if err != nil {
		return nil, err
	}
	return &Conn{
		settings: s, clock: clock, opener: opener,
		log:   log.With().Str("component", "modbus").Str("address", s.Address).Logger(),
		queue: make(chan *request, queueSize), done: make(chan struct{}),
		backoff: util.Backoff{Min: BackoffMin, Max: BackoffMax},
	}, nil
}

// Start runs the worker until Stop. Calling it again has no effect.
func (c *Conn) Start() {
	c.start.Do(func() {
		c.wg.Add(1)
		go c.run()
	})
}

// Stop ends the worker, waits for it and closes the client. It is idempotent.
func (c *Conn) Stop() {
	c.once.Do(func() { close(c.done) })
	c.wg.Wait()
}

// Read queues a request and waits for its result or ctx.
func (c *Conn) Read(ctx context.Context, unit uint8, fn Function, start, count uint16,
) ([]uint16, error) {
	req := &request{
		ctx: ctx, unit: unit, fn: fn, start: start, count: count,
		reply: make(chan result, 1),
	}
	select {
	case c.queue <- req:
	case <-c.done:
		return nil, ErrStopped
	case <-ctx.Done():
		return nil, fmt.Errorf("modbus: queue: %w", ctx.Err())
	}
	select {
	case r := <-req.reply:
		return r.words, r.err
	case <-c.done:
		return nil, ErrStopped
	case <-ctx.Done():
		return nil, fmt.Errorf("modbus: wait: %w", ctx.Err())
	}
}

func (c *Conn) run() {
	defer c.wg.Done()
	defer c.closeClient()
	for {
		select {
		case <-c.done:
			return
		case req := <-c.queue:
			if req.ctx.Err() != nil {
				continue // caller gave up while queued
			}
			words, err := c.serve(req)
			req.reply <- result{words: words, err: err}
		}
	}
}

// serve answers one request. A transport error on a connection that was already open
// (a gateway dropping one request, a half-open socket after a quiet period) gets one
// immediate retry on a fresh connection: most hiccups recover inside the same poll instead
// of failing it and waiting out a backoff. Only a fresh connection that fails backs off.
func (c *Conn) serve(req *request) ([]uint16, error) {
	if c.inBackoff() {
		return nil, fmt.Errorf("%w: backoff until %s", ErrUnavailable,
			c.backoff.Next().Format(time.TimeOnly))
	}
	words, retry, err := c.attempt(req)
	if retry && req.ctx.Err() == nil {
		c.log.Debug().Err(err).Msg("request failed on an open connection, reconnecting")
		words, _, err = c.attempt(req)
	}
	return words, err
}

// attempt runs the request once; retry reports a transport error on a reused connection.
func (c *Conn) attempt(req *request) ([]uint16, bool, error) {
	ctx, cancel := context.WithTimeout(req.ctx, c.settings.Timeout)
	defer cancel()
	reused := c.client != nil
	if !reused {
		if err := c.open(ctx); err != nil {
			return nil, false, err
		}
	}
	b, err := c.client.ReadRegisters(ctx, req.unit, req.fn, req.start, req.count)
	if err != nil {
		err = c.classify(err, reused)
		return nil, reused && errors.Is(err, ErrUnavailable), err
	}
	if len(b) != bytesPerRegister*int(req.count) {
		return nil, false, fmt.Errorf("%w: got %d bytes for %d registers", ErrException, len(b),
			req.count)
	}
	c.backoff.Reset()
	return toWords(b), false, nil
}

func (c *Conn) inBackoff() bool {
	return c.client == nil && !c.backoff.Ready(c.clock.Now())
}

func (c *Conn) open(ctx context.Context) error {
	cl, err := c.opener.Open(ctx, c.settings)
	if err != nil {
		c.fail()
		return fmt.Errorf("%w: open: %w", ErrUnavailable, err)
	}
	c.client = cl
	return nil
}

// classify maps an error: exceptions keep the connection, everything else drops it. A
// reused connection is dropped without backoff (the caller retries at once); a fresh one
// that fails backs off.
func (c *Conn) classify(err error, reused bool) error {
	var mbErr *gridx.Error
	if errors.As(err, &mbErr) {
		if mbErr.ExceptionCode == gridx.ExceptionCodeIllegalDataAddress {
			return fmt.Errorf("%w: %w", ErrIllegalAddress, err)
		}
		return fmt.Errorf("%w: %w", ErrException, err)
	}
	c.closeClient()
	if !reused {
		c.fail()
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// fail schedules the next attempt with exponential backoff 1 s → 30 s.
func (c *Conn) fail() {
	d := c.backoff.Fail(c.clock.Now())
	c.log.Debug().Dur("backoff", d).Msg("connection failed, backing off")
}

func (c *Conn) closeClient() {
	if c.client == nil {
		return
	}
	if err := c.client.Close(); err != nil {
		c.log.Debug().Err(err).Msg("close client")
	}
	c.client = nil
}

func toWords(b []byte) []uint16 {
	out := make([]uint16, len(b)/bytesPerRegister)
	for i := range out {
		out[i] = binary.BigEndian.Uint16(b[2*i:])
	}
	return out
}
