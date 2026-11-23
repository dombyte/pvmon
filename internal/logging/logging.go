// Package logging builds the injected zerolog.Logger: JSON to stdout and, when a file is
// configured, additionally to a file rotated by timberjack. There is no global logger.
package logging

import (
	"fmt"
	"io"
	"time"

	"github.com/DeRuina/timberjack"
	"github.com/rs/zerolog"
)

const hoursPerDay = 24

// Settings configures the logger; app maps the log.* and app.log_level keys onto it.
type Settings struct {
	Level       string
	File        string
	MaxSizeMB   int
	MaxBackups  int
	MaxAge      time.Duration
	RotateAt    []string
	Compression string
}

// Logger is the constructed logger plus the closer of its optional file.
type Logger struct {
	zerolog.Logger
	closer io.Closer
}

// Close closes the log file, if any.
func (l *Logger) Close() error {
	if l.closer == nil {
		return nil
	}
	if err := l.closer.Close(); err != nil {
		return fmt.Errorf("logging: close file: %w", err)
	}
	return nil
}

// New builds a logger writing JSON to stdout and, with s.File set, to the rotated file.
func New(stdout io.Writer, s Settings) (*Logger, error) {
	level, err := zerolog.ParseLevel(s.Level)
	if err != nil || s.Level == "" {
		return nil, fmt.Errorf("logging: invalid level %q", s.Level)
	}
	if s.File == "" {
		return &Logger{Logger: newLogger(stdout, level)}, nil
	}
	file := &timberjack.Logger{
		Filename:    s.File,
		MaxSize:     s.MaxSizeMB,
		MaxBackups:  s.MaxBackups,
		MaxAge:      int(s.MaxAge / (hoursPerDay * time.Hour)),
		RotateAt:    s.RotateAt,
		Compression: s.Compression,
	}
	w := zerolog.MultiLevelWriter(stdout, file)
	return &Logger{Logger: newLogger(w, level), closer: file}, nil
}

func newLogger(w io.Writer, level zerolog.Level) zerolog.Logger {
	return zerolog.New(w).Level(level).With().Timestamp().Logger()
}
