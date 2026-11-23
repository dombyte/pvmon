package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/frontend"
	"github.com/dombyte/pvmon/internal/buildinfo"
	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/derive"
	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/router"
	"github.com/dombyte/pvmon/internal/http/server"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/logging"
	"github.com/dombyte/pvmon/internal/maintenance"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
	"github.com/dombyte/pvmon/internal/ws"
)

// Lifecycle constants (spec 8.3).
const (
	ShutdownDeadline = 30 * time.Second
	sweepInterval    = 5 * time.Second
	graceIntervals   = 5
	dirMode          = 0o750
)

// SourceFactory builds the sources and transport of one connection's collector.
type SourceFactory func(s *site.Site, c *site.Connection, clock util.Clock, log zerolog.Logger,
) (map[string]collector.Source, collector.Transport, error)

// ServeOptions configure Serve; zero Listen, Clock, Sources and Probe use the real ones.
type ServeOptions struct {
	Loaded   *Loaded
	Build    buildinfo.Info
	Stdout   io.Writer
	Listen   func(addr string) (net.Listener, error)
	Clock    util.Clock
	Sources  SourceFactory
	Probe    ProbeFactory
	Location *time.Location
}

// Serve runs the server until ctx ends (exit 0) or a fatal health error (ErrHealthFatal).
func Serve(ctx context.Context, o ServeOptions) error {
	o = withDefaults(o)
	cfg := o.Loaded.Result.Config
	log, err := CreateLogger(o.Stdout, o.Loaded)
	if err != nil {
		return err
	}
	defer closeLog(log)
	log.Info().Str("version", o.Build.Version).Str("commit", o.Build.Commit).Msg("starting")
	for _, w := range o.Loaded.Result.Warnings {
		log.Warn().Msg(w)
	}
	lock, err := lockShared(cfg.Storage.Path)
	if err != nil {
		return err
	}
	defer releaseLock(lock, log.Logger)
	return run(ctx, o, log.Logger)
}

func withDefaults(o ServeOptions) ServeOptions {
	if o.Listen == nil {
		o.Listen = func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) }
	}
	if o.Clock == nil {
		o.Clock = util.NewRealClock()
	}
	if o.Sources == nil {
		o.Sources = RealSources
	}
	if o.Probe == nil {
		o.Probe = RealProbe
	}
	if o.Location == nil {
		o.Location = LocalLocation()
	}
	return o
}

// CreateLogger builds the logger from app.log_level and log.*.
func CreateLogger(stdout io.Writer, l *Loaded) (*logging.Logger, error) {
	c := l.Result.Config
	log, err := logging.New(stdout, logging.Settings{
		Level: c.App.LogLevel, File: c.Log.File, MaxSizeMB: c.Log.MaxSize.MB(),
		MaxBackups: c.Log.MaxBackups, MaxAge: c.Log.MaxAge.D(), RotateAt: c.Log.RotateAt,
		Compression: c.Log.Compression,
	})
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return log, nil
}

func closeLog(l *logging.Logger) {
	_ = l.Close() // nothing is left to log a failure to
}

func lockShared(dbPath string) (*maintenance.Lock, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), dirMode); err != nil {
		return nil, fmt.Errorf("app: data directory: %w", err)
	}
	lock, err := maintenance.AcquireShared(dbPath)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return lock, nil
}

func releaseLock(l *maintenance.Lock, log zerolog.Logger) {
	if err := l.Release(); err != nil {
		log.Warn().Err(err).Msg("release lock")
	}
}

// run wires everything after the lock is held.
func run(ctx context.Context, o ServeOptions, log zerolog.Logger) error {
	db, err := database.Open(ctx, o.Loaded.Result.Config.Storage.Path)
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Warn().Err(cerr).Msg("close database")
		}
	}()
	st, err := migrated(ctx, o, db)
	if err != nil {
		return err
	}
	cs, snap, loaded, err := openConfig(ctx, o, st, log)
	if err != nil {
		return err
	}
	o.Loaded = loaded
	root, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	sup, err := health.NewSupervisor(o.Clock, sweepInterval, cancel, log)
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	d, err := newLiveDeps(o, st)
	if err != nil {
		return err
	}
	rt := newRuntime(runtimeDeps{o: o, sup: sup, d: d, db: db, cs: cs, snap: snap}, log)
	rt.register()
	return runServer(root, o, rt, log)
}

// migrated migrates the database after a verified backup and opens the storage.
func migrated(ctx context.Context, o ServeOptions, db *sql.DB) (*storage.Storage, error) {
	bs := backupSettings(o.Loaded)
	backup := func(ctx context.Context) error {
		_, err := database.Backup(ctx, db, bs, o.Clock.Now())
		return err
	}
	if err := database.Migrate(ctx, db, backup, o.Clock.Now()); err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	st, err := storage.New(db, o.Location)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return st, nil
}

func backupSettings(l *Loaded) database.BackupSettings {
	dir := filepath.Dir(l.Result.Config.Storage.Path)
	return database.BackupSettings{
		Dir:     filepath.Join(dir, "backups"),
		KeyFile: filepath.Join(dir, "secret.key"), MaxBackups: l.Result.Config.Storage.MaxBackups,
	}
}

// liveDeps are the shared parts the components and handlers meet through.
type liveDeps struct {
	st    *storage.Storage
	bus   *eventbus.Bus
	cache *livecache.Cache
	hub   *util.Slot[*ws.Hub] // current WebSocket hub, replaced on restart
}

func newLiveDeps(o ServeOptions, st *storage.Storage) (liveDeps, error) {
	bus := eventbus.New()
	cache, err := livecache.New(bus, o.Clock)
	if err != nil {
		return liveDeps{}, fmt.Errorf("app: %w", err)
	}
	return liveDeps{st: st, bus: bus, cache: cache, hub: &util.Slot[*ws.Hub]{}}, nil
}

// collectorFactory builds the collector of a connection and records its transport, so the
// setup probe reads through it.
func collectorFactory(o ServeOptions, conn *site.Connection, deps collector.Deps,
	buses *busRegistry,
) health.Factory {
	return func() (health.Component, error) {
		srcs, tr, err := o.Sources(o.Loaded.Site, conn, o.Clock, deps.Log)
		if err != nil {
			return nil, err
		}
		buses.set(conn.Address, tr)
		deps.Transport = tr
		return collector.New(collector.Settings{
			Site: o.Loaded.Site, Connection: conn.Address,
			Sources: srcs, Location: o.Location,
		}, deps)
	}
}

func longestInterval(s *site.Site, c *site.Connection) time.Duration {
	var longest time.Duration
	for _, id := range c.Devices {
		longest = max(longest, s.Devices[id].Interval)
	}
	return longest
}

// runServer serves HTTP and supervises until the root context ends, then shuts down.
func runServer(root context.Context, o ServeOptions, rt *runtime, log zerolog.Logger,
) error {
	sup := rt.sup
	cfg := o.Loaded.Result.Config
	ln, err := o.Listen(":" + strconv.Itoa(cfg.App.Port))
	if err != nil {
		return fmt.Errorf("app: listen: %w", err)
	}
	h, err := httpHandler(root, o, rt, log)
	if err != nil {
		return err
	}
	srv := server.New(h, cfg.App.RequestTimeout.D())
	go func() {
		if err := srv.Serve(ln); err != nil {
			sup.Fail(err)
		}
	}()
	supDone := make(chan error, 1)
	go func() { supDone <- sup.Run(root) }()
	log.Info().Str("addr", ln.Addr().String()).Msg("listening")
	<-root.Done()
	return shutdown(root, srv, supDone, log)
}

func handlers(o ServeOptions, sup *health.Supervisor, d liveDeps, ah router.AuthHandlers,
	log zerolog.Logger,
) router.Handlers {
	critical := map[string]bool{}
	for _, m := range o.Loaded.Site.Meters {
		if m.Critical {
			critical[m.Device] = true
		}
	}
	hs, _ := service.NewHealth(sup, o.Clock, service.HealthSettings{ //nolint:errcheck // deps set
		Sweep: sweepInterval, OfflineAfter: o.Loaded.Result.Config.Status.OfflineAfter.D(),
		Critical: critical,
	})
	//nolint:errcheck // deps set
	ss, _ := service.NewSite(o.Loaded.Site, o.Location, d.st.Read(), sup)
	//nolint:errcheck // deps set
	dv, _ := derive.New(o.Loaded.Site, d.st.Read(), d.st.Derive(), o.Clock, o.Location)
	//nolint:errcheck // deps set
	es, _ := service.NewEnergy(dv)
	//nolint:errcheck // deps set
	eh, _ := handler.NewEnergy(es, o.Clock, o.Location, log)
	return router.Handlers{
		Energy: eh, Gauge: gaugeHandler(dv, o.Location, log),
		Report:  reportHandler(o, dv, log),
		Devices: deviceHandlers(o, d.st.Read(), sup, log),
		Health:  handler.NewHealth(hs, log), Version: handler.NewVersion(o.Build, log),
		Site: handler.NewSite(ss, log), Auth: ah,
		WS: wsHandler(d, log),
		UI: handler.NewStatic(frontend.FS(), log),
	}
}

func gaugeHandler(dv *derive.Service, loc *time.Location, log zerolog.Logger) http.Handler {
	gs, _ := service.NewGauge(dv, loc)      //nolint:errcheck // deps set
	gh, _ := handler.NewGauge(gs, loc, log) //nolint:errcheck // deps set
	return gh
}

func reportHandler(o ServeOptions, dv *derive.Service, log zerolog.Logger) http.Handler {
	//nolint:errcheck // deps set
	rs, _ := service.NewReports(o.Loaded.Site, dv, o.Clock, o.Location)
	info := handler.ReportInfo{
		Site: o.Loaded.Site.Name, Version: o.Build.String(), Location: o.Location,
	}
	rh, _ := handler.NewReport(rs, info, o.Clock, log) //nolint:errcheck // deps set
	return rh
}

func deviceHandlers(o ServeOptions, st service.StatusReader, sup service.SnapshotSource,
	log zerolog.Logger,
) router.DeviceHandlers {
	//nolint:errcheck // deps set
	ds, _ := service.NewDevices(o.Loaded.Site, sup, st, o.Location)
	dh, _ := handler.NewDevices(ds, o.Clock, o.Location, log) //nolint:errcheck // deps set
	return router.DeviceHandlers{
		List: dh.List, Get: dh.Get, Status: dh.Status, Overview: dh.Overview,
	}
}

// shutdown runs the single-phase shutdown within ShutdownDeadline.
func shutdown(root context.Context, srv *server.Server, supDone <-chan error,
	log zerolog.Logger,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(root), ShutdownDeadline)
	defer cancel()
	log.Info().Msg("shutting down")
	err := srv.Shutdown(ctx)
	select {
	case serr := <-supDone:
		err = errors.Join(err, serr)
	case <-ctx.Done():
		err = errors.Join(err, errors.New("app: shutdown deadline exceeded"))
	}
	if cause := context.Cause(root); errors.Is(cause, health.ErrHealthFatal) {
		return errors.Join(cause, err)
	}
	return err
}

func wsHandler(d liveDeps, log zerolog.Logger) *ws.Handler {
	//nolint:errcheck // the slot is always set
	h, _ := ws.NewHandler(d.hub, log.With().Str("component", "ws").Logger())
	return h
}
