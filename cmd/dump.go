package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/maintenance"
	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util"
)

type dumpArgs struct {
	config   string
	device   string
	count    int
	interval time.Duration
	out      string
}

func parseDump(e env) (dumpArgs, error) {
	fs, cfg := newFlags("dump", e.stderr)
	var a dumpArgs
	fs.StringVar(&a.device, "device", "", "device ID")
	fs.IntVar(&a.count, "count", 1, "number of polls")
	fs.DurationVar(&a.interval, "interval", 0, "time between polls (default: device interval)")
	fs.StringVar(&a.out, "o", "", "output file (default stdout)")
	if err := fs.Parse(e.args); err != nil || a.device == "" || a.count < 1 ||
		fs.NArg() > 0 {
		return a, fmt.Errorf("dump --device id [--count N --interval D] [-o file]: %w", errUsage)
	}
	a.config = *cfg
	return a, nil
}

// runDump implements "pvmon dump" (spec 4.5, 13): stopped server, exclusive lock.
func runDump(e env) error {
	a, err := parseDump(e)
	if err != nil {
		return err
	}
	l, err := loadConfig(a.config)
	if err != nil {
		return err
	}
	dev, ok := l.Site.Devices[a.device]
	if !ok {
		return fmt.Errorf("dump: unknown device %q: %w", a.device, errUsage)
	}
	if a.interval == 0 {
		a.interval = dev.Interval
	}
	lock, err := lockDB(l.Result.Config.Storage.Path)
	if err != nil {
		return err
	}
	defer releaseLock(lock, e.stderr)
	return dumpDevice(e, l, a)
}

func lockDB(dbPath string) (*maintenance.Lock, error) {
	const dirMode = 0o750
	if err := os.MkdirAll(filepath.Dir(dbPath), dirMode); err != nil {
		return nil, fmt.Errorf("data directory: %w", err)
	}
	lock, err := maintenance.AcquireExclusive(dbPath)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	return lock, nil
}

func releaseLock(l *maintenance.Lock, stderr io.Writer) {
	if err := l.Release(); err != nil {
		writeLine(stderr, err.Error())
	}
}

func dumpDevice(e env, l *app.Loaded, a dumpArgs) (err error) {
	log, err := cliLogger(e.stderr, l)
	if err != nil {
		return err
	}
	clock := util.NewRealClock()
	dev := l.Site.Devices[a.device]
	var mb *modbus.Conn
	if conn := l.Site.Connections[dev.Connection]; app.IsModbus(conn) {
		if mb, err = app.CreateModbusConn(conn, clock, log.Logger); err != nil {
			return err
		}
		mb.Start()
		defer mb.Stop()
	}
	f, err := app.CreateFetcher(l.Site, dev, mb, clock, log.Logger)
	if err != nil {
		return err
	}
	w, closeOut, err := output(a.out, e.stdout)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeOut()) }()
	job := maintenance.DumpJob{
		Source: f, Profile: dev.Profile.Profile, Clock: clock,
		Log: log.Logger,
	}
	return maintenance.Dump(e.ctx, job, maintenance.DumpOptions{
		Count:    a.count,
		Interval: a.interval,
	}, w)
}

func output(path string, stdout io.Writer) (io.Writer, func() error, error) {
	if path == "" {
		return stdout, func() error { return nil }, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("dump output: %w", err)
	}
	return f, f.Close, nil
}
