-- Status changes keep only the raw code: level and texts come from the profile at read
-- time, so a profile fix also corrects the history (spec 12). The register "@connection"
-- holds the device connection (0 online, 1 offline, 2 stopped). Old rows are dropped.
DROP TABLE status_event;
CREATE TABLE status_event (
  device_id TEXT NOT NULL, register TEXT NOT NULL, at INTEGER NOT NULL,
  code INTEGER NOT NULL,
  PRIMARY KEY (device_id, register, at)
) WITHOUT ROWID;
