package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	rawInterval     = time.Minute
	rawRetention    = 24 * time.Hour
	bucketInterval  = 15 * time.Minute
	bucketRetention = 30 * 24 * time.Hour
)

// Sample is one reading of a subject (a host or a managed resource). A nil
// field was not available, which is different from zero.
type Sample struct {
	CPU       *float64
	MemUsed   *int64
	MemTotal  *int64
	DiskUsed  *int64
	DiskTotal *int64
	Load1     *float64
}

func (s Sample) empty() bool {
	return s.CPU == nil && s.MemUsed == nil && s.DiskUsed == nil && s.Load1 == nil
}

// SubjectKey names what is measured: kind is "server", "application",
// "database" or "stack".
func SubjectKey(kind, id string) string { return kind + ":" + id }

// HistoryStore persists metric points and answers range queries.
type HistoryStore struct{ DB *sql.DB }

// Record stores one reading per subject at the given minute. A repeated
// minute replaces the previous point.
func (h *HistoryStore) Record(ctx context.Context, at time.Time, samples map[string]Sample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO metric_points_raw
		(subject,ts,cpu,mem_used,mem_total,disk_used,disk_total,load1) VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	ts := at.Truncate(rawInterval).Unix()
	for subject, s := range samples {
		if s.empty() {
			continue
		}
		if _, err := statement.ExecContext(ctx, subject, ts, s.CPU, s.MemUsed, s.MemTotal, s.DiskUsed, s.DiskTotal, s.Load1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Compact aggregates every finished 15-minute bucket of raw points, then
// applies the retention of both resolutions. It is idempotent: a bucket that
// already exists is left as it is, and raw points are only deleted after the
// buckets that cover them were written, in the same transaction.
func (h *HistoryStore) Compact(ctx context.Context, now time.Time) error {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	currentBucket := now.Unix() - now.Unix()%int64(bucketInterval.Seconds())
	if _, err := tx.ExecContext(ctx, aggregateStatement, int64(bucketInterval.Seconds()), currentBucket); err != nil {
		return fmt.Errorf("aggregate metrics: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM metric_points_raw WHERE ts<?`, now.Add(-rawRetention).Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM metric_points_15m WHERE ts<?`, now.Add(-bucketRetention).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// The bucket start is ts - ts % 900 (?1 is the bucket length). A bucket is
// finished when it starts before the current one (?2).
const aggregateStatement = `INSERT OR IGNORE INTO metric_points_15m
 (subject,ts,cpu_avg,cpu_max,mem_avg,mem_max,mem_total,disk_avg,disk_max,disk_total,load_avg,load_max)
 SELECT subject, ts-ts%?1 AS bucket, AVG(cpu), MAX(cpu), AVG(mem_used), MAX(mem_used), MAX(mem_total),
        AVG(disk_used), MAX(disk_used), MAX(disk_total), AVG(load1), MAX(load1)
 FROM metric_points_raw WHERE ts-ts%?1 < ?2 GROUP BY subject, bucket`

// Series holds columnar arrays: index i of every non-nil array belongs to
// T[i]. Columns with no value at all are nil and omitted from the JSON.
// Maximum columns exist only at 15-minute resolution.
type Series struct {
	T          []int64    `json:"t"`
	CPU        []*float64 `json:"cpu,omitempty"`
	CPUMax     []*float64 `json:"cpu_max,omitempty"`
	MemUsed    []*float64 `json:"mem_used,omitempty"`
	MemUsedMax []*float64 `json:"mem_used_max,omitempty"`
	MemTotal   []*float64 `json:"mem_total,omitempty"`
	DiskUsed   []*float64 `json:"disk_used,omitempty"`
	DiskMax    []*float64 `json:"disk_used_max,omitempty"`
	DiskTotal  []*float64 `json:"disk_total,omitempty"`
	Load       []*float64 `json:"load,omitempty"`
	LoadMax    []*float64 `json:"load_max,omitempty"`
}

// History is the answer to a range query.
type History struct {
	Range      string `json:"range"`
	StepSecond int    `json:"step_seconds"`
	From       int64  `json:"from"`
	To         int64  `json:"to"`
	Series     Series `json:"series"`
}

// Query returns the points of a subject for a validated range.
func (h *HistoryStore) Query(ctx context.Context, subject string, r Range, now time.Time) (History, error) {
	from := now.Add(-r.Duration).Unix()
	query, step := rawQuery, int(rawInterval.Seconds())
	if r.Resolution == bucketInterval {
		query, step = bucketQuery, int(bucketInterval.Seconds())
	}
	rows, err := h.DB.QueryContext(ctx, query, subject, from, now.Unix())
	if err != nil {
		return History{}, err
	}
	defer rows.Close()
	columns := 6
	if r.Resolution == bucketInterval {
		columns = 10
	}
	history := History{Range: r.Name, StepSecond: step, From: from, To: now.Unix()}
	builder := newSeriesBuilder(columns)
	for rows.Next() {
		var ts int64
		values := make([]sql.NullFloat64, columns)
		targets := make([]any, 0, columns+1)
		targets = append(targets, &ts)
		for i := range values {
			targets = append(targets, &values[i])
		}
		if err := rows.Scan(targets...); err != nil {
			return History{}, err
		}
		builder.add(ts, values)
	}
	if err := rows.Err(); err != nil {
		return History{}, err
	}
	history.Series = builder.series(r.Resolution == bucketInterval)
	return history, nil
}

var rawQuery = `SELECT ts,cpu,mem_used,mem_total,disk_used,disk_total,load1 FROM metric_points_raw
 WHERE subject=? AND ts>=? AND ts<=? ORDER BY ts`

var bucketQuery = `SELECT ts,cpu_avg,cpu_max,mem_avg,mem_max,mem_total,disk_avg,disk_max,disk_total,load_avg,load_max
 FROM metric_points_15m WHERE subject=? AND ts>=? AND ts<=? ORDER BY ts`

type seriesBuilder struct {
	t       []int64
	columns [][]*float64
}

func newSeriesBuilder(columns int) *seriesBuilder {
	return &seriesBuilder{columns: make([][]*float64, columns)}
}

func (b *seriesBuilder) add(ts int64, values []sql.NullFloat64) {
	b.t = append(b.t, ts)
	for i, value := range values {
		var cell *float64
		if value.Valid {
			v := value.Float64
			cell = &v
		}
		b.columns[i] = append(b.columns[i], cell)
	}
}

// series maps the columns of either query to the named arrays. Raw order:
// cpu, mem, mem_total, disk, disk_total, load. Bucket order: cpu avg/max, mem
// avg/max, mem_total, disk avg/max, disk_total, load avg/max.
func (b *seriesBuilder) series(buckets bool) Series {
	column := func(i int) []*float64 { return presentOrNil(b.columns[i]) }
	s := Series{T: b.t}
	if s.T == nil {
		s.T = []int64{}
	}
	if buckets {
		s.CPU, s.CPUMax, s.MemUsed, s.MemUsedMax, s.MemTotal = column(0), column(1), column(2), column(3), column(4)
		s.DiskUsed, s.DiskMax, s.DiskTotal, s.Load, s.LoadMax = column(5), column(6), column(7), column(8), column(9)
		return s
	}
	s.CPU, s.MemUsed, s.MemTotal, s.DiskUsed, s.DiskTotal, s.Load = column(0), column(1), column(2), column(3), column(4), column(5)
	return s
}

func presentOrNil(values []*float64) []*float64 {
	for _, v := range values {
		if v != nil {
			return values
		}
	}
	return nil
}

// Range is a validated chart window and the resolution that serves it.
type Range struct {
	Name       string
	Duration   time.Duration
	Resolution time.Duration
}

var ranges = []Range{
	{"1h", time.Hour, rawInterval},
	{"6h", 6 * time.Hour, rawInterval},
	{"24h", 24 * time.Hour, rawInterval},
	{"7d", 7 * 24 * time.Hour, bucketInterval},
	{"30d", 30 * 24 * time.Hour, bucketInterval},
}

// ParseRange resolves a range name; the second result is false when unknown.
func ParseRange(name string) (Range, bool) {
	for _, r := range ranges {
		if r.Name == name {
			return r, true
		}
	}
	return Range{}, false
}

// RangeNames lists the accepted ranges for error messages.
func RangeNames() string {
	names := make([]string, len(ranges))
	for i, r := range ranges {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}
