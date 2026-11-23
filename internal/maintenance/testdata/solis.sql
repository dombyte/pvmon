-- A small solis v3 database for import-solis tests (spec 7.8). The import clock stands on
-- 2026-04-01 (Europe/Berlin); 2026-03-29 is the DST day (23 hours).
CREATE TABLE daily_values (id INTEGER PRIMARY KEY AUTOINCREMENT, date DATE NOT NULL,
  register_key TEXT NOT NULL, value REAL NOT NULL, raw_value REAL NOT NULL,
  UNIQUE(register_key, date));
CREATE TABLE monthly_values (id INTEGER PRIMARY KEY AUTOINCREMENT, month TEXT NOT NULL,
  register_key TEXT NOT NULL, value REAL NOT NULL, raw_value REAL NOT NULL,
  UNIQUE(register_key, month));
CREATE TABLE yearly_values (id INTEGER PRIMARY KEY AUTOINCREMENT, year TEXT NOT NULL,
  register_key TEXT NOT NULL, value REAL NOT NULL, raw_value REAL NOT NULL,
  UNIQUE(register_key, year));
CREATE TABLE total_values (id INTEGER PRIMARY KEY AUTOINCREMENT,
  register_key TEXT NOT NULL UNIQUE, value REAL NOT NULL, raw_value REAL NOT NULL,
  timestamp DATETIME NOT NULL);
CREATE TABLE error_data (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME NOT NULL,
  register_key TEXT NOT NULL, raw_value REAL NOT NULL, string_value TEXT,
  UNIQUE(register_key, timestamp));
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);

INSERT INTO daily_values (date, register_key, value, raw_value) VALUES
  ('2026-03-28', 'pv_energy_daily', 10.5, 10.5),
  ('2026-03-29', 'pv_energy_daily', 12.25, 12.25),
  ('2026-03-30', 'pv_energy_daily', 0, 0),
  ('2026-03-31', 'pv_energy_daily', 8.0, 8.0),
  ('2026-04-01', 'pv_energy_daily', 3.0, 3.0),
  ('2026-03-30', 'battery_charge_daily', 4.0, 4.0),
  ('2026-03-31', 'battery_charge_daily', -1.0, -1.0),
  ('2026-03-31', 'household_energy_daily', 7.0, 7.0);

INSERT INTO monthly_values (month, register_key, value, raw_value) VALUES
  ('2026-01', 'pv_energy_monthly', 0, 0),
  ('2026-02', 'pv_energy_monthly', 150.0, 150.0),
  ('2026-03', 'pv_energy_monthly', 40.75, 40.75),
  ('2026-04', 'pv_energy_monthly', 3.0, 3.0),
  ('2026-03', 'household_energy_monthly', 99.0, 99.0);

INSERT INTO yearly_values (year, register_key, value, raw_value) VALUES
  ('2026', 'pv_energy_yearly', 33.75, 33.75);

INSERT INTO total_values (register_key, value, raw_value, timestamp) VALUES
  ('pv_energy_total', 43.75, 43.75, '2026-04-01T10:00:00+02:00'),
  ('battery_charge_total', 2.0, 2.0, '2026-04-01T10:00:00+02:00');

INSERT INTO error_data (timestamp, register_key, raw_value) VALUES
  ('2026-03-30T08:00:00.000Z', 'solis_status', 4112),
  ('2026-03-30T08:05:00.000Z', 'solis_status', 3),
  ('2026-03-30T08:05:00.000Z', 'mystery_fault', 1),
  ('2026-04-01T06:00:00.000Z', 'solis_status', 4112);

INSERT INTO meta (key, value) VALUES ('v3_cutover_date', '2026-03-01');
