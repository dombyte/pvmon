-- Bucket values become fixed-point integers in 1/1000 of their unit (mWh, mW, 0.001 %)
-- instead of REAL: about 25 % smaller and exact sums (spec 7.1, 7.2). The derived cache is
-- dropped and recomputed lazily; this also removes the "no data" rows older versions cached
-- for every day before the first stored slot.
CREATE TABLE b15_gauge_fixed (
  series_id INTEGER NOT NULL, slot INTEGER NOT NULL,
  avg INTEGER NOT NULL, min INTEGER NOT NULL, max INTEGER NOT NULL,  -- 1/1000 unit
  cover INTEGER NOT NULL,                -- covered seconds 0..900
  PRIMARY KEY (series_id, slot)
) WITHOUT ROWID;
INSERT INTO b15_gauge_fixed
  SELECT series_id, slot, CAST(round(avg * 1000) AS INTEGER),
    CAST(round(min * 1000) AS INTEGER), CAST(round(max * 1000) AS INTEGER), cover
  FROM b15_gauge ORDER BY series_id, slot;
DROP TABLE b15_gauge;
ALTER TABLE b15_gauge_fixed RENAME TO b15_gauge;

CREATE TABLE b15_energy_fixed (
  series_id INTEGER NOT NULL, slot INTEGER NOT NULL,
  wh INTEGER NOT NULL,                   -- mWh in this bucket (>= 0)
  counter INTEGER,                       -- raw counter at bucket end in mWh; NULL if integrated
  flags INTEGER NOT NULL DEFAULT 0,      -- 1 estimated, 2 gap, 4 reset_seen, 8 imported
  PRIMARY KEY (series_id, slot)
) WITHOUT ROWID;
INSERT INTO b15_energy_fixed
  SELECT series_id, slot, CAST(round(wh * 1000) AS INTEGER),
    CAST(round(counter * 1000) AS INTEGER), flags
  FROM b15_energy ORDER BY series_id, slot;
DROP TABLE b15_energy;
ALTER TABLE b15_energy_fixed RENAME TO b15_energy;

DELETE FROM derived_cache;
