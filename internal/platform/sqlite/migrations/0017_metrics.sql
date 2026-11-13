-- Metrics history. One table family for hosts ("server:<id>") and managed
-- resources ("application:<id>", "database:<id>", "stack:<id>"), so there is
-- a single sampler, compactor and query path. Timestamps are unix seconds.
-- Raw points are 1 minute apart and kept 24 h; 15-minute aggregates (average
-- and maximum) are kept 30 days. Columns that do not apply stay NULL. The
-- primary key (subject, ts) serves both "server over time" and "resource over
-- time" queries. There is deliberately no index on ts alone: in a WITHOUT ROWID
-- table it would repeat the subject in every entry, and the retention delete
-- scans a table bounded by the retention itself.
CREATE TABLE metric_points_raw (
 subject TEXT NOT NULL, ts INTEGER NOT NULL,
 cpu REAL, mem_used INTEGER, mem_total INTEGER, disk_used INTEGER, disk_total INTEGER, load1 REAL,
 PRIMARY KEY(subject, ts)
) WITHOUT ROWID;
CREATE TABLE metric_points_15m (
 subject TEXT NOT NULL, ts INTEGER NOT NULL,
 cpu_avg REAL, cpu_max REAL, mem_avg REAL, mem_max INTEGER, mem_total INTEGER,
 disk_avg REAL, disk_max INTEGER, disk_total INTEGER, load_avg REAL, load_max REAL,
 PRIMARY KEY(subject, ts)
) WITHOUT ROWID;
