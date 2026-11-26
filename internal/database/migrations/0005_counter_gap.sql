-- Start of the first outage since the base of a counter (unix ms, 0 = none). The delta that
-- moves the base is estimated from there on, also when it is booked in a later run
-- (spec 5.3).
ALTER TABLE counter_state ADD COLUMN gap INTEGER NOT NULL DEFAULT 0;
