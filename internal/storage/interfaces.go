package storage

import (
	"context"
	"errors"
	"fmt"
)

// SlotSeconds is the length of a bucket (spec 5.2): slot = unix seconds / 900.
const SlotSeconds = 900

// Domain is a write domain (spec 7.5).
type Domain int

// Write domains.
const (
	DomainCollector Domain = iota + 1
	DomainDerive
	DomainConfig
	DomainAuth
	DomainCLI
)

func (d Domain) String() string {
	return [...]string{"none", "collector", "derive", "config", "auth", "cli"}[d]
}

// DomainError is returned for a write outside the caller's domain.
type DomainError struct {
	Domain Domain
	Table  string
}

func (e *DomainError) Error() string {
	return fmt.Sprintf("storage: %s may not write %s", e.Domain, e.Table)
}

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("storage: not found")

// Bucket flags (spec 7.1).
const (
	FlagEstimated = 1
	FlagGap       = 2
	FlagResetSeen = 4
	FlagImported  = 8
)

// SeriesKey identifies a series by meter and channel.
type SeriesKey struct {
	Meter   string
	Channel string
}

// SeriesDef describes a series to ensure.
type SeriesDef struct {
	SeriesKey
	Kind   int
	Origin int
	Hidden bool
	Unit   string
}

// CounterState is the persisted state of one counter input (spec 5.3).
type CounterState struct {
	SeriesID   int64
	Raw        *float64 // last raw value; nil before the first reading
	At         int64    // unix ms of Raw
	Seen       int64    // unix ms of the last poll that read the counter (restart point)
	Remainder  float64  // energy below 1 Wh not yet booked
	Resolution float64
	ResetAt    int64 // unix ms of the last confirmed daily reset of its group; 0 = none
}

// GaugeBucket is a (partial) 15-minute gauge aggregate; partials of a slot are merged.
type GaugeBucket struct {
	SeriesID int64
	Slot     int64
	Avg      float64
	Min      float64
	Max      float64
	Cover    int // covered seconds
}

// EnergyBucket is energy booked into a slot; Wh of a flush is added to the stored value.
type EnergyBucket struct {
	SeriesID int64
	Slot     int64
	Wh       float64
	Counter  *float64 // raw counter at the end of the slot part, nil when integrated
	Flags    int
}

// Batch is everything one collector writes in one transaction.
type Batch struct {
	Gauges   []GaugeBucket
	Energy   []EnergyBucket
	Counters []CounterState
	// TodayStart is the first slot of the current local day; writes to earlier slots
	// invalidate the derived cache of their periods in the same transaction.
	TodayStart int64
}

// ConnectionRegister is the status register that holds the connection of a device; it
// never clashes with a profile status name (those are identifiers).
const ConnectionRegister = "@connection"

// Codes of ConnectionRegister.
const (
	ConnectionOnline  int64 = 0
	ConnectionOffline int64 = 1
	ConnectionStopped int64 = 2 // pvmon stopped polling: no data until the next change
)

// StatusEvent is a status change of a device register.
type StatusEvent struct {
	Device   string
	Register string
	At       int64 // unix ms
	Code     int64 // raw code; level and texts come from the profile (spec 12)
}

// CollectorStore is the write interface of collectors.
type CollectorStore interface {
	EnsureSeries(ctx context.Context, slot int64, defs []SeriesDef) (map[SeriesKey]int64, error)
	CounterStates(ctx context.Context, ids []int64) (map[int64]CounterState, error)
	Flush(ctx context.Context, b Batch) error
	AddStatus(ctx context.Context, events []StatusEvent) error
	LastStatus(ctx context.Context, device string) (map[string]StatusEvent, error)
}

// EnergyTotal is the summed energy of one series over a slot range.
type EnergyTotal struct {
	Wh      float64
	Buckets int
	Flags   int // OR of all bucket flags
}

// ReadStore is the read interface for services and replay tests.
type ReadStore interface {
	SumEnergy(ctx context.Context, seriesID, from, to int64) (EnergyTotal, error)
	EnergyBuckets(ctx context.Context, seriesID, from, to int64) ([]EnergyBucket, error)
	GaugeBuckets(ctx context.Context, seriesID, from, to int64) ([]GaugeBucket, error)
	SeriesID(ctx context.Context, key SeriesKey) (int64, error)
	// FirstSlot returns the earliest slot with any stored bucket; ok is false without data.
	FirstSlot(ctx context.Context) (slot int64, ok bool, err error)
	// SumEnergyMany sums several series over slots [from, to) in one grouped query.
	// Closed counts only the buckets before closedBefore without the gap flag, so a
	// caller can compare it with the number of closed slots. Series without buckets are
	// absent from the map.
	SumEnergyMany(ctx context.Context, ids []int64, from, to, closedBefore int64,
	) (map[int64]EnergySum, error)
	// Carry returns the carried energy (spec 7.4) per series ID; absent = 0.
	Carry(ctx context.Context, ids []int64) (map[int64]float64, error)
	// StatusEvents returns a device's status changes in [from, to) (unix ms), newest first
	// and at most limit, followed by the change of each register active at from.
	StatusEvents(ctx context.Context, device string, from, to int64, limit int,
	) ([]StatusEvent, error)
}

// EnergySum is the grouped sum of one series over a slot range.
type EnergySum struct {
	Wh        float64
	Buckets   int  // buckets in the range
	Closed    int  // buckets before closedBefore without FlagGap
	Estimated bool // any bucket flagged FlagEstimated or FlagGap
	Imported  bool // any bucket flagged FlagImported: one bucket holds a whole day (7.8)
}

// DeriveStore is the write interface of derive: the derived cache only (spec 7.3).
type DeriveStore interface {
	// CacheGet returns the cached values of keys stored under configRev.
	CacheGet(ctx context.Context, keys []string, configRev int64) (map[string]string, error)
	// CacheGen returns the cache generation, taken before the buckets are read.
	CacheGen() int64
	// CachePut stores values (JSON) under configRev, replacing older entries, unless a past
	// slot was written since gen (the values may be stale then).
	CachePut(ctx context.Context, configRev, gen int64, entries map[string]string) error
}

// ErrAlreadyImported means the database already holds an import; a second one would book
// the same history twice.
var ErrAlreadyImported = errors.New("storage: database already holds an import")

// ImportSeries is the history of one energy series written by an import.
type ImportSeries struct {
	Def     SeriesDef
	Buckets []EnergyBucket // SeriesID is ignored; slots must be free
	Carry   float64        // Wh before the first bucket (spec 7.4); 0 = none
}

// ImportBatch is everything an import writes in one transaction.
type ImportBatch struct {
	Source string // recorded as meta import_source
	Series []ImportSeries
	Status []StatusEvent
}

// ImportStore is the write interface of the import CLI job (domain cli).
type ImportStore interface {
	// ImportSource returns the recorded import source, "" when nothing was imported.
	ImportSource(ctx context.Context) (string, error)
	// FirstSlot returns the earliest stored bucket slot; ok is false without data.
	FirstSlot(ctx context.Context) (slot int64, ok bool, err error)
	// Import writes the batch in one transaction and drops the derived cache.
	Import(ctx context.Context, b ImportBatch) error
}

// ErrConflict is returned when a write collides with an existing row (duplicate name).
var ErrConflict = errors.New("storage: conflict")

// ErrLastUser is returned when the last user would be deleted (spec 10).
var ErrLastUser = errors.New("storage: the last user cannot be deleted")

// User is an admin account (spec 10).
type User struct {
	ID        int64
	Name      string
	PwHash    string
	Lang      string
	CreatedAt int64 // unix ms
	LastLogin int64 // unix ms; 0 = never
}

// Session is a login session; only the SHA-256 of its token is stored.
type Session struct {
	TokenHash []byte
	UserID    int64
	CreatedAt int64 // unix ms
	LastSeen  int64 // unix ms
}

// APIToken is an API token; only the SHA-256 of its secret is stored.
type APIToken struct {
	ID         string
	SecretHash []byte
	Name       string
	Scope      string
	UserID     int64 // 0 = created by a CLI job
	CreatedAt  int64 // unix ms
	ExpiresAt  int64 // unix ms; 0 = never
	LastUsed   int64 // unix ms; 0 = never
}

// AuditEntry is one row of the audit log (spec 10).
type AuditEntry struct {
	ID     int64
	At     int64 // unix ms
	Actor  string
	Action string
	Detail string // JSON, never secrets
}

// LoginRecord is what a successful login writes in one transaction.
type LoginRecord struct {
	UserID  int64
	At      int64  // unix ms
	PwHash  string // upgraded hash; "" keeps the stored one
	Session Session
	Audit   AuditEntry
}

// AuthStore is the write interface of the auth service and the user/token CLI jobs. Every
// change writes its audit entry in the same transaction.
type AuthStore interface {
	CountUsers(ctx context.Context) (int, error)
	// CreateUser inserts u; onlyFirst refuses with ErrConflict once any user exists.
	CreateUser(ctx context.Context, u User, onlyFirst bool, a AuditEntry) (int64, error)
	UserByName(ctx context.Context, name string) (User, error)
	UserByID(ctx context.Context, id int64) (User, error)
	Users(ctx context.Context) ([]User, error)
	// DeleteUser removes a user with its sessions and tokens; ErrLastUser for the last one.
	DeleteUser(ctx context.Context, id int64, a AuditEntry) error
	// SetPassword replaces the hash and deletes every session of the user.
	SetPassword(ctx context.Context, id int64, hash string, a AuditEntry) error
	RecordLogin(ctx context.Context, r LoginRecord) error
	Session(ctx context.Context, tokenHash []byte) (Session, error)
	TouchSession(ctx context.Context, tokenHash []byte, at int64) error
	DeleteSession(ctx context.Context, tokenHash []byte, a AuditEntry) error
	// PurgeSessions deletes sessions last seen before idleBefore or created before
	// createdBefore (unix ms).
	PurgeSessions(ctx context.Context, idleBefore, createdBefore int64) error
	CreateToken(ctx context.Context, t APIToken, a AuditEntry) error
	Token(ctx context.Context, id string) (APIToken, error)
	Tokens(ctx context.Context) ([]APIToken, error)
	DeleteToken(ctx context.Context, id string, a AuditEntry) error
	TouchToken(ctx context.Context, id string, at int64) error
	AddAudit(ctx context.Context, a AuditEntry) error
	// AuditLog returns the entries in [from, to) (unix ms), newest first, at most limit.
	AuditLog(ctx context.Context, from, to int64, limit int) ([]AuditEntry, error)
}

// ConfigObject is one object of the UI configuration (spec 7.1).
type ConfigObject struct {
	Kind      string // connection | device | meter
	ID        string
	Body      string // JSON; secrets as enc:v1:<base64>
	UpdatedAt int64  // unix ms
	UpdatedBy string
}

// ConfigKey names one object of the UI configuration.
type ConfigKey struct{ Kind, ID string }

// ConfigState is the stored UI configuration with its revision (0 = never saved).
type ConfigState struct {
	Rev      int64
	Objects  []ConfigObject
	Settings map[string]string // key → JSON value
}

// ConfigChange is one saved UI change. It is written with revision BaseRev+1, its history
// snapshot and its audit entry in one transaction.
type ConfigChange struct {
	BaseRev  int64 // ErrConflict when the stored revision differs
	Put      []ConfigObject
	Delete   []ConfigKey
	Set      map[string]string // setting key → JSON value
	Unset    []string
	At       int64 // unix ms
	By       string
	Snapshot string // JSON of the whole UI configuration after the change
	Audit    AuditEntry
}

// ConfigStore is the interface of the config service and the config CLI jobs.
type ConfigStore interface {
	ConfigState(ctx context.Context) (ConfigState, error)
	// ApplyConfig writes c and returns the new revision.
	ApplyConfig(ctx context.Context, c ConfigChange) (int64, error)
}
