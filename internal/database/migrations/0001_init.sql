-- Schema v1 (spec 7.1).
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID;
-- schema_version, created_at, purged_before, import_source

CREATE TABLE series (
  id          INTEGER PRIMARY KEY,
  meter_id    TEXT    NOT NULL,          -- 'site' for site quantities
  channel     TEXT    NOT NULL,          -- power, soc, energy_in, energy_out, grid_import_raw ...
  kind        INTEGER NOT NULL,          -- 1 gauge, 2 energy
  origin      INTEGER NOT NULL,          -- 1 measured, 2 integrated, 3 derived share
  hidden      INTEGER NOT NULL DEFAULT 0,
  unit        TEXT    NOT NULL,
  active_from INTEGER NOT NULL,          -- slot
  active_to   INTEGER,                   -- NULL = active
  UNIQUE (meter_id, channel, active_from)
);

CREATE TABLE b15_gauge (
  series_id INTEGER NOT NULL, slot INTEGER NOT NULL,
  avg REAL NOT NULL, min REAL NOT NULL, max REAL NOT NULL,
  cover INTEGER NOT NULL,                -- covered seconds 0..900
  PRIMARY KEY (series_id, slot)
) WITHOUT ROWID;

CREATE TABLE b15_energy (
  series_id INTEGER NOT NULL, slot INTEGER NOT NULL,
  wh REAL NOT NULL,                      -- energy in this bucket (>= 0)
  counter REAL,                          -- raw counter at bucket end; NULL if integrated
  flags INTEGER NOT NULL DEFAULT 0,      -- 1 estimated, 2 gap, 4 reset_seen, 8 imported
  PRIMARY KEY (series_id, slot)
) WITHOUT ROWID;

CREATE TABLE counter_state (
  series_id INTEGER PRIMARY KEY,
  raw REAL, at INTEGER NOT NULL,         -- last raw value, unix ms
  remainder REAL NOT NULL DEFAULT 0,     -- below 1 Wh
  resolution REAL, reset INTEGER NOT NULL DEFAULT 0  -- unix ms of the last daily reset, 0 none
);

CREATE TABLE status_event (
  device_id TEXT NOT NULL, register TEXT NOT NULL, at INTEGER NOT NULL,
  code INTEGER NOT NULL, level INTEGER NOT NULL,  -- 0 ok, 1 notice, 2 fault
  PRIMARY KEY (device_id, register, at)
) WITHOUT ROWID;

CREATE TABLE carry (
  key TEXT PRIMARY KEY,                  -- series id or derived key
  wh REAL NOT NULL, before_slot INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE derived_cache (
  key TEXT PRIMARY KEY,                  -- 'site/consumption|month|2026-10'
  config_rev INTEGER NOT NULL,
  value TEXT NOT NULL                    -- JSON: value, complete, estimated, missing_days
) WITHOUT ROWID;

CREATE TABLE config_object (
  kind TEXT NOT NULL, id TEXT NOT NULL,  -- connection | device | meter
  body TEXT NOT NULL,                    -- JSON; secrets as enc:v1:<base64>
  updated_at INTEGER NOT NULL, updated_by TEXT,
  PRIMARY KEY (kind, id)
) WITHOUT ROWID;
CREATE TABLE config_setting (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID;
CREATE TABLE config_history (rev INTEGER PRIMARY KEY, at INTEGER NOT NULL, by TEXT, snapshot TEXT NOT NULL);

CREATE TABLE user (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pw_hash TEXT NOT NULL, lang TEXT, created_at INTEGER NOT NULL, last_login INTEGER
);
CREATE TABLE session (
  token_hash BLOB PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL, last_seen INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE api_token (
  id TEXT PRIMARY KEY, secret_hash BLOB NOT NULL, name TEXT NOT NULL, scope TEXT NOT NULL,
  user_id INTEGER REFERENCES user(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL, expires_at INTEGER, last_used INTEGER
) WITHOUT ROWID;
CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY, at INTEGER NOT NULL, actor TEXT, action TEXT NOT NULL, detail TEXT
);
