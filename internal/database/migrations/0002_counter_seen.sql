-- Last poll that read a counter (unix ms): a restarted collector resumes reset detection
-- from there instead of from the last change of the value (spec 5.3).
ALTER TABLE counter_state ADD COLUMN seen INTEGER NOT NULL DEFAULT 0;
