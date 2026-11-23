package config

// Numeric defaults (spec 3).
const (
	DefaultPort        = 8080
	DefaultMaxBackups  = 3
	DefaultLogBackups  = 5
	DefaultSerialSpeed = 9600
	DefaultDataBits    = 8
)

// Defaults returns the default of every global key (flat koanf paths). A test enforces that
// every key of Config has one, here or in ObjectDefaults.
func Defaults() map[string]any {
	return map[string]any{
		"app.port":                DefaultPort,
		"app.log_level":           "info",
		"app.request_timeout":     "30s",
		"app.trusted_proxies":     []string{},
		"auth.public_read":        true,
		"auth.session_idle":       "7d",
		"auth.session_max":        "30d",
		"storage.path":            "./data/pvmon.db",
		"storage.retention":       "10y",
		"storage.backup_interval": "24h",
		"storage.max_backups":     DefaultMaxBackups,
		"status.offline_after":    "15m",
		"log.file":                "",
		"log.max_size":            "50MB",
		"log.max_backups":         DefaultLogBackups,
		"log.max_age":             "30d",
		"log.rotate_at":           []string{},
		"log.compression":         "gzip",
		"site.name":               "Home",
		"site.interval":           "5s",
	}
}

// ObjectDefaults returns the defaults applied to every object of a kind (connections,
// devices, meters), keyed by kind and then by the key inside the object. Empty strings
// mean "taken from elsewhere" (connection timeout, profile read mode, profile netting).
func ObjectDefaults() map[string]map[string]any {
	return map[string]map[string]any{
		"connections": {
			"address": "", "timeout": "2s", "speed": DefaultSerialSpeed,
			"data_bits": DefaultDataBits, "parity": "N", "stop_bits": 1,
		},
		"devices": {
			"connection": "", "address": "", "unit": 1, "profile": "", "interval": "0s",
			"timeout": "0s", "read": "", "rollover": "", "netting": "",
			"history": []string{}, "insecure_tls": false,
			"auth.user": "", "auth.password": "",
		},
		"meters": {
			"device": "", "source": "", "role": "", "category": "", "parent": "",
			"label": "", "kwp": 0.0, "capacity": "0Wh", "critical": false,
		},
	}
}
