// Package config loads the layered configuration (defaults → DB → file → env), decodes it
// into typed sections and validates its structure. Domain rules are injected as Rules by
// internal/app; this package imports no domain package (AGENTS.md section 3).
package config

// Config is the effective configuration. Field tags are the YAML/koanf keys.
type Config struct {
	App         App                   `koanf:"app"`
	Auth        Auth                  `koanf:"auth"`
	Storage     Storage               `koanf:"storage"`
	Status      Status                `koanf:"status"`
	Log         Log                   `koanf:"log"`
	Site        Site                  `koanf:"site"`
	Connections map[string]Connection `koanf:"connections"`
	Devices     map[string]Device     `koanf:"devices"`
	Meters      map[string]Meter      `koanf:"meters"`
}

// App holds the app.* settings.
type App struct {
	Port           int      `koanf:"port"`
	LogLevel       string   `koanf:"log_level"`
	RequestTimeout Duration `koanf:"request_timeout"`
	TrustedProxies []string `koanf:"trusted_proxies"`
}

// Auth holds the auth.* settings.
type Auth struct {
	PublicRead  bool     `koanf:"public_read"`
	SessionIdle Duration `koanf:"session_idle"`
	SessionMax  Duration `koanf:"session_max"`
}

// Storage holds the storage.* settings.
type Storage struct {
	Path           string   `koanf:"path"`
	Retention      Duration `koanf:"retention"`
	BackupInterval Duration `koanf:"backup_interval"`
	MaxBackups     int      `koanf:"max_backups"`
}

// Status holds the status.* settings.
type Status struct {
	OfflineAfter Duration `koanf:"offline_after"`
}

// Log holds the log.* settings (spec 3).
type Log struct {
	File        string   `koanf:"file"`
	MaxSize     Size     `koanf:"max_size"`
	MaxBackups  int      `koanf:"max_backups"`
	MaxAge      Duration `koanf:"max_age"`
	RotateAt    []string `koanf:"rotate_at"`
	Compression string   `koanf:"compression"`
}

// Site holds the site.* settings.
type Site struct {
	Name string `koanf:"name"`
	// Interval is the poll interval of every device that does not set its own.
	Interval Duration `koanf:"interval"`
}

// Connection is one physical access path (spec 2).
type Connection struct {
	Address  string   `koanf:"address"`
	Timeout  Duration `koanf:"timeout"`
	Speed    int      `koanf:"speed"`
	DataBits int      `koanf:"data_bits"`
	Parity   string   `koanf:"parity"`
	StopBits int      `koanf:"stop_bits"`
}

// Device is a polled device: a connection (by ID or inline address) plus profile.
type Device struct {
	Connection string `koanf:"connection"`
	Address    string `koanf:"address"`
	Unit       int    `koanf:"unit"`
	Profile    string `koanf:"profile"`
	// Interval overrides site.interval for this device; 0 = the site interval.
	Interval    Duration   `koanf:"interval"`
	Timeout     Duration   `koanf:"timeout"`
	Read        string     `koanf:"read"`
	Rollover    string     `koanf:"rollover"`
	Netting     string     `koanf:"netting"`
	History     []string   `koanf:"history"`
	InsecureTLS bool       `koanf:"insecure_tls"`
	Auth        DeviceAuth `koanf:"auth"`
}

// DeviceAuth holds device credentials; the password is a Secret.
type DeviceAuth struct {
	User     string `koanf:"user"`
	Password Secret `koanf:"password"`
}

// Meter is a logical measuring point provided by one device block (spec 2).
type Meter struct {
	Device   string  `koanf:"device"`
	Source   string  `koanf:"source"`
	Role     string  `koanf:"role"`
	Category string  `koanf:"category"`
	Parent   string  `koanf:"parent"`
	Label    string  `koanf:"label"`
	KWp      float64 `koanf:"kwp"`
	Capacity Energy  `koanf:"capacity"`
	Critical bool    `koanf:"critical"`
}
