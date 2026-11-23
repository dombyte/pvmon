package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/maintenance"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

type importArgs struct {
	config string
	db     string
	mapf   string
}

func parseImport(e env) (importArgs, error) {
	fs, cfg := newFlags("import-solis", e.stderr)
	var a importArgs
	fs.StringVar(&a.db, "db", "", "solis database (read-only)")
	fs.StringVar(&a.mapf, "map", "", "import map (YAML)")
	if err := fs.Parse(e.args); err != nil || a.db == "" || a.mapf == "" || fs.NArg() > 0 {
		return a, fmt.Errorf("import-solis --db solis.db --map map.yaml: %w", errUsage)
	}
	a.config = *cfg
	return a, nil
}

// runImportSolis implements "pvmon import-solis" (spec 7.8, 13): stopped server,
// exclusive lock, verified backup, one transaction.
func runImportSolis(e env) error {
	a, err := parseImport(e)
	if err != nil {
		return err
	}
	l, err := loadConfig(a.config)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(a.mapf)
	if err != nil {
		return fmt.Errorf("import map: %w", err)
	}
	m, err := maintenance.ParseSolisMap(data)
	if err != nil {
		return err
	}
	series, err := m.Resolve(l.Site)
	if err != nil {
		return err
	}
	lock, err := lockDB(l.Result.Config.Storage.Path)
	if err != nil {
		return err
	}
	defer releaseLock(lock, e.stderr)
	return importSolis(e, l, a, maintenance.ImportJob{Map: m, Series: series})
}

func importSolis(e env, l *app.Loaded, a importArgs, job maintenance.ImportJob) (err error) {
	src, err := database.OpenReadOnly(e.ctx, a.db)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, src.Close()) }()
	if job.Source, err = maintenance.NewSolisDB(e.ctx, src); err != nil {
		return err
	}
	clock := util.NewRealClock()
	db, err := app.OpenJobDB(e.ctx, l, clock.Now())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	loc := app.LocalLocation()
	st, err := storage.New(db, loc)
	if err != nil {
		return err
	}
	job.Target, job.Location, job.Clock = st.Import(), loc, clock
	job.Name = "solis:" + filepath.Base(a.db)
	if err := backupFirst(e, l, db, job); err != nil {
		return err
	}
	rep, err := maintenance.ImportSolis(e.ctx, job)
	if err != nil {
		return err
	}
	writeReport(e.stdout, rep)
	return nil
}

// backupFirst refuses a second import before it costs a backup, then backs up.
func backupFirst(e env, l *app.Loaded, db *sql.DB, job maintenance.ImportJob) error {
	src, err := job.Target.ImportSource(e.ctx)
	if err != nil {
		return err
	}
	if src != "" {
		return fmt.Errorf("%w (%s)", storage.ErrAlreadyImported, src)
	}
	backup, err := app.BackupJobDB(e.ctx, l, db, job.Clock.Now())
	if err != nil {
		return err
	}
	writeLine(e.stdout, "backup: "+backup)
	return nil
}

// writeReport prints the import summary (spec 7.8).
func writeReport(w io.Writer, r *maintenance.ImportReport) {
	writeLine(w, "imported days before "+r.Cutoff)
	for _, s := range r.Series {
		writeLine(w, seriesLine(s))
	}
	if len(r.Unmapped) > 0 {
		writeLine(w, "not imported (not in map): "+strings.Join(r.Unmapped, ", "))
	}
	writeLine(w, fmt.Sprintf("status changes: %d", r.Status))
	if len(r.StatusKeys) > 0 {
		writeLine(w, "status not imported (not in map): "+strings.Join(r.StatusKeys, ", "))
	}
	for _, p := range r.Mismatches {
		writeLine(w, fmt.Sprintf("%-7s %-22s %-7s solis %.2f kWh, daily sum %.2f kWh "+
			"(%d days): only the daily values are imported",
			p.Level, p.Key, p.Period, p.Stored, p.Daily, p.Days))
	}
}

func seriesLine(s maintenance.SeriesReport) string {
	line := fmt.Sprintf("%-22s -> %-20s %4d days %10.2f kWh", s.Key, s.Target, s.Days, s.KWh)
	if s.Later > 0 {
		line += fmt.Sprintf(", %d later days left to pvmon", s.Later)
	}
	if s.Negative > 0 {
		line += fmt.Sprintf(", %d negative days skipped", s.Negative)
	}
	if s.CarryKWh > 0 {
		line += fmt.Sprintf(", carry %.2f kWh", s.CarryKWh)
	}
	if s.TotalBelow {
		line += ", solis total below daily sum (no carry)"
	}
	return line
}
