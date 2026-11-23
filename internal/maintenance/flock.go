// Package maintenance holds the CLI jobs that run with the server stopped: the shared lock
// next to the database, dumps, backups, backfill, import and data correction (spec 13).
package maintenance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrLocked means another process (normally the running server) holds the lock.
var ErrLocked = errors.New("maintenance: database is in use (server running?)")

// Lock is a held flock on <db>.lock.
type Lock struct{ f *os.File }

// LockPath returns the lock file of a database path.
func LockPath(dbPath string) string { return dbPath + ".lock" }

// AcquireShared takes the server's shared lock; jobs then cannot take the exclusive one.
func AcquireShared(dbPath string) (*Lock, error) { return acquire(dbPath, syscall.LOCK_SH) }

// AcquireExclusive takes the lock of a "stopped" job; it fails with ErrLocked while the
// server or another job holds the lock.
func AcquireExclusive(dbPath string) (*Lock, error) { return acquire(dbPath, syscall.LOCK_EX) }

func acquire(dbPath string, how int) (*Lock, error) {
	const mode = 0o600
	f, err := os.OpenFile(LockPath(dbPath), os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		return nil, fmt.Errorf("maintenance: open lock: %w", err)
	}
	fd := int(f.Fd())
	if err := syscall.Flock(fd, how|syscall.LOCK_NB); err != nil {
		_ = f.Close() // lock not taken; the close error adds nothing
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("maintenance: flock: %w", err)
	}
	return &Lock{f: f}, nil
}

// Release unlocks and closes the lock file.
func (l *Lock) Release() error {
	if err := l.f.Close(); err != nil { // closing the fd releases the flock
		return fmt.Errorf("maintenance: release lock: %w", err)
	}
	return nil
}
