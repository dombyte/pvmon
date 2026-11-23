package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/configstore"
	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/middleware"
	"github.com/dombyte/pvmon/internal/http/router"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/sitecalc"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
	"github.com/dombyte/pvmon/internal/ws"
)

// collectorPrefix names the supervised collectors ("collector:<address>").
const collectorPrefix = "collector:"

// runtime owns what a change of the UI configuration rebuilds without restart (spec 3):
// the collectors, the site calculator, the backup job and the HTTP routes. It also
// implements handler.ConfigService. Changes are serialised by mu.
type runtime struct {
	o   ServeOptions // Loaded is the configuration at start; cur is the current one
	sup *health.Supervisor
	d   liveDeps
	db  *sql.DB
	cs  *configstore.Service
	log zerolog.Logger

	mu   sync.Mutex
	snap *configstore.Snapshot
	cur  util.Slot[*Loaded]
	mux  util.Slot[http.Handler]
	auth *auth.Service

	buses   busRegistry // transports of the running collectors, for the setup probe
	probeMu sync.Mutex
}

// openConfig loads the UI configuration (creating secret.key on first start) and the
// effective configuration with it.
func openConfig(ctx context.Context, o ServeOptions, st *storage.Storage, log zerolog.Logger,
) (*configstore.Service, *configstore.Snapshot, *Loaded, error) {
	cs, err := CreateConfigStore(o.Loaded, st, o.Clock, true)
	if err != nil {
		return nil, nil, nil, err
	}
	snap, err := cs.Load(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("app: %w", err)
	}
	for k, obj := range snap.Objects {
		if len(obj.Unreadable) > 0 {
			log.Warn().Str("kind", k.Kind).Str("id", k.ID).Strs("secrets", obj.Unreadable).
				Msg("stored secrets cannot be decrypted (secret.key changed); enter them again")
		}
	}
	l, err := o.Loaded.WithDB(snap.Layer(), snap.Rev)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("app: with the UI configuration from the database: %w",
			err)
	}
	return cs, snap, l, nil
}

// CreateConfigStore builds the config service; create makes secret.key when missing
// (server), CLI jobs read without creating it.
func CreateConfigStore(l *Loaded, st *storage.Storage, clock util.Clock, create bool,
) (*configstore.Service, error) {
	key := filepath.Join(filepath.Dir(l.Result.Config.Storage.Path), "secret.key")
	c, err := configstore.LoadKey(key, create, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	secrets := map[string][]string{}
	for sec, paths := range config.SecretPaths() {
		secrets[strings.TrimSuffix(sec, "s")] = paths
	}
	cs, err := configstore.New(configstore.Settings{
		Secrets: secrets, SettingKeys: config.UISettingKeys(),
	}, configstore.Deps{Store: st.Config(), Sealer: c, Clock: clock})
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return cs, nil
}

// runtimeDeps are the parts the runtime is built from.
type runtimeDeps struct {
	o    ServeOptions
	sup  *health.Supervisor
	d    liveDeps
	db   *sql.DB
	cs   *configstore.Service
	snap *configstore.Snapshot
}

func newRuntime(rd runtimeDeps, log zerolog.Logger) *runtime {
	r := &runtime{
		o: rd.o, sup: rd.sup, d: rd.d, db: rd.db, cs: rd.cs, snap: rd.snap, log: log,
	}
	r.cur.Store(rd.o.Loaded)
	return r
}

// current returns the configuration in effect.
func (r *runtime) current() *Loaded {
	l, _ := r.cur.Load() // stored in newRuntime
	return l
}

// register adds every component for the configuration at start; Run starts them.
func (r *runtime) register() {
	l := r.current()
	r.setCollectors(nil, l)
	r.setSitecalc(l)
	r.setBackup(l)
	r.sup.Add("ws", func() (health.Component, error) {
		return ws.NewHub(r.current().Site, r.d.cache, r.d.bus, r.o.Clock, r.d.hub)
	}, graceIntervals*sitecalc.Refresh)
}

// apply puts a new configuration into effect: only what changed is rebuilt, the routes
// always (they carry the site and the settings), and clients get site_changed.
func (r *runtime) apply(prev, next *Loaded) {
	r.cur.Store(next)
	if siteChanged(prev, next) {
		r.setCollectors(prev, next)
		r.setSitecalc(next)
	}
	if prev.Result.Config.Storage != next.Result.Config.Storage {
		r.setBackup(next)
	}
	r.mux.Store(r.routes(next))
	r.d.bus.Publish(eventbus.SiteChanged{Rev: next.Site.Rev, Keys: next.Site.LiveKeys()})
	r.log.Info().Int64("config_rev", next.Site.Rev).Msg("configuration applied")
}

// siteChanged reports whether anything the collectors or sitecalc use has changed.
func siteChanged(a, b *Loaded) bool {
	ca, cb := a.Result.Config, b.Result.Config
	return ca.Site.Interval != cb.Site.Interval ||
		!reflect.DeepEqual(ca.Connections, cb.Connections) ||
		!reflect.DeepEqual(ca.Devices, cb.Devices) || !reflect.DeepEqual(ca.Meters, cb.Meters)
}

// setCollectors rebuilds one collector per connection of next that has devices; devices
// that left the site are forgotten by health.
func (r *runtime) setCollectors(prev, next *Loaded) {
	s := next.Site
	r.dropCollectors(s)
	if prev != nil {
		r.forgetDevices(prev, next)
	}
	o := r.o
	o.Loaded = next
	for addr, conn := range s.Connections {
		if len(conn.Devices) == 0 {
			continue // a collector without devices has nothing to poll
		}
		r.sup.Set(collectorPrefix+addr, collectorFactory(o, conn, r.collectorDeps(), &r.buses),
			graceIntervals*longestInterval(s, conn))
	}
}

// dropCollectors removes the collectors of connections that are gone or have no device left.
func (r *runtime) dropCollectors(s *site.Site) {
	for _, name := range r.sup.Names() {
		addr, ok := strings.CutPrefix(name, collectorPrefix)
		if c, keep := s.Connections[addr]; ok && (!keep || len(c.Devices) == 0) {
			r.sup.Remove(name)
			r.buses.drop(addr)
		}
	}
}

func (r *runtime) forgetDevices(prev, next *Loaded) {
	for id := range prev.Site.Devices {
		if _, ok := next.Site.Devices[id]; !ok {
			r.sup.ForgetDevice(id)
		}
	}
}

func (r *runtime) setSitecalc(l *Loaded) {
	r.sup.Set("sitecalc", func() (health.Component, error) {
		return sitecalc.New(l.Site, r.d.cache, r.d.bus, r.o.Clock)
	}, graceIntervals*sitecalc.Refresh)
}

func (r *runtime) setBackup(l *Loaded) {
	r.sup.Set("backup", func() (health.Component, error) {
		return &backupJob{
			db: r.db, s: backupSettings(l), interval: l.Result.Config.Storage.BackupInterval.D(),
			clock: r.o.Clock, log: r.log.With().Str("component", "backup").Logger(),
		}, nil
	}, backupCheck*graceIntervals)
}

// routes builds the mux for a configuration; the middleware chain around it is fixed.
func (r *runtime) routes(l *Loaded) http.Handler {
	o := r.o
	o.Loaded = l
	cfg := l.Result.Config
	//nolint:errcheck // deps set
	ah, _ := handler.NewAuth(r.auth, cfg.Auth.PublicRead, o.Clock, o.Location, r.log)
	h := handlers(o, r.sup, r.d, authHandlers(ah), r.log)
	h.Config = r.configHandlers(l)
	return router.New(h, router.Guards{
		Read: middleware.Read(cfg.Auth.PublicRead, r.log), Admin: middleware.Admin(r.log),
	})
}

// ServeHTTP dispatches to the routes of the current configuration.
func (r *runtime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h, _ := r.mux.Load() // stored before the server starts
	h.ServeHTTP(w, req)
}
