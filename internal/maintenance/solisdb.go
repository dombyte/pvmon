package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotSolis means the source database has no solis daily_values table.
var ErrNotSolis = errors.New("maintenance: not a solis database (no daily_values table)")

// Period levels of a solis database; the register key of a series is "<base>_<level>"
// (pv_energy_daily, pv_energy_monthly, ...).
const (
	LevelDaily   = "daily"
	LevelMonthly = "monthly"
	LevelYearly  = "yearly"
	LevelTotal   = "total"
)

// SolisValue is one stored solis value: Base is the key without its level suffix, Period
// the day (2006-01-02), month (2006-01) or year (2006); empty for totals.
type SolisValue struct {
	Base   string
	Period string
	KWh    float64
}

// SolisStatus is one row of the solis status log.
type SolisStatus struct {
	Key  string
	At   time.Time
	Code int64
}

// SolisSource reads a solis database.
type SolisSource interface {
	// Values returns all values of a level, ordered by key and period.
	Values(ctx context.Context, level string) ([]SolisValue, error)
	// Status returns the status log in time order; unparsable timestamps fail.
	Status(ctx context.Context) ([]SolisStatus, error)
}

// SolisDB reads the tables of a solis database (schema v3, read-only).
type SolisDB struct{ db *sql.DB }

// NewSolisDB checks that db is a solis database.
func NewSolisDB(ctx context.Context, db *sql.DB) (*SolisDB, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master
		WHERE type='table' AND name='daily_values'`).Scan(&n)
	if err != nil {
		return nil, fmt.Errorf("maintenance: read solis schema: %w", err)
	}
	if n == 0 {
		return nil, ErrNotSolis
	}
	return &SolisDB{db: db}, nil
}

// levelQueries maps a level to its query; the period column differs per table. Periods
// are cast to TEXT because the driver turns DATE columns into times.
func levelQueries() map[string]string {
	const q = `SELECT register_key, CAST(%s AS TEXT), value FROM %s
		ORDER BY register_key, %[1]s`
	return map[string]string{
		LevelDaily:   fmt.Sprintf(q, "date", "daily_values"),
		LevelMonthly: fmt.Sprintf(q, "month", "monthly_values"),
		LevelYearly:  fmt.Sprintf(q, "year", "yearly_values"),
		LevelTotal:   `SELECT register_key, '', value FROM total_values ORDER BY register_key`,
	}
}

// Values implements SolisSource.
func (s *SolisDB) Values(ctx context.Context, level string) ([]SolisValue, error) {
	q, ok := levelQueries()[level]
	if !ok {
		return nil, fmt.Errorf("maintenance: unknown solis level %q", level)
	}
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("maintenance: read solis %s: %w", level, err)
	}
	defer rows.Close()
	var out []SolisValue
	for rows.Next() {
		var (
			key string
			v   SolisValue
		)
		if err := rows.Scan(&key, &v.Period, &v.KWh); err != nil {
			return nil, fmt.Errorf("maintenance: scan solis %s: %w", level, err)
		}
		if base, ok := strings.CutSuffix(key, "_"+level); ok {
			v.Base = base
			out = append(out, v)
		}
	}
	return out, wrapRows(rows.Err())
}

// Status implements SolisSource.
func (s *SolisDB) Status(ctx context.Context) ([]SolisStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT register_key, CAST(timestamp AS TEXT),
		raw_value FROM error_data ORDER BY timestamp`)
	if err != nil {
		return nil, fmt.Errorf("maintenance: read solis status: %w", err)
	}
	defer rows.Close()
	var out []SolisStatus
	for rows.Next() {
		var (
			st    SolisStatus
			stamp string
			code  float64
		)
		if err := rows.Scan(&st.Key, &stamp, &code); err != nil {
			return nil, fmt.Errorf("maintenance: scan solis status: %w", err)
		}
		if st.At, err = time.Parse(time.RFC3339Nano, stamp); err != nil {
			return nil, fmt.Errorf("maintenance: solis status %s: %w", st.Key, err)
		}
		st.Code = int64(code)
		out = append(out, st)
	}
	return out, wrapRows(rows.Err())
}

func wrapRows(err error) error {
	if err != nil {
		return fmt.Errorf("maintenance: rows: %w", err)
	}
	return nil
}
