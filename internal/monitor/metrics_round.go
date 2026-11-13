package monitor

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"pier/internal/server"
)

// recordRound writes hosts once and application totals once, after all collectors
// finish. Destination subjects are internal, never accepted from an API parameter.
// A missing destination leaves a gap in the total rather than an understated sum.
func (s *Sampler) recordRound(ctx context.Context, at time.Time, results map[string]server.Metrics) {
	samples := map[string]Sample{}
	applications := map[string]map[string]Sample{}
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT id FROM applications`)
	if err != nil {
		s.Logger.Warn("metrics: inventory", "error", err)
		return
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return
		}
		applications[id] = map[string]Sample{}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		samples[SubjectKey("server", id)] = hostSample(results[id])
		for subject, point := range resourceSamples(results[id].Containers) {
			if strings.HasPrefix(subject, "application:") {
				appID := strings.TrimPrefix(subject, "application:")
				destinations, err := s.Store.applicationDestinations(ctx, appID)
				if err != nil {
					continue
				} // fail closed for unknown or untrusted labels
				trusted := false
				for _, destination := range destinations {
					if destination == id {
						trusted = true
					}
				}
				if !trusted {
					continue
				}
				if applications[appID] == nil {
					applications[appID] = map[string]Sample{}
				}
				applications[appID][id] = point
				// JSON encoding is unambiguous even when IDs contain punctuation.
				key, _ := json.Marshal([]string{appID, id})
				samples["application_destination:"+string(key)] = point
			} else {
				samples[subject] = point
			}
		}
	}
	for appID, points := range applications {
		destinations, err := s.Store.applicationDestinations(ctx, appID)
		if err != nil {
			continue
		}
		var cpu float64
		var memory int64
		cpuOK, memOK := true, true
		for _, id := range destinations {
			point, ok := points[id]
			if !ok || point.CPU == nil {
				cpuOK = false
			} else {
				cpu += *point.CPU
			}
			if !ok || point.MemUsed == nil {
				memOK = false
			} else {
				memory += *point.MemUsed
			}
		}
		total := Sample{}
		if cpuOK {
			total.CPU = &cpu
		}
		if memOK {
			total.MemUsed = &memory
		}
		samples[SubjectKey("application", appID)] = total
	}
	if err := s.Store.Record(ctx, at, samples); err != nil {
		s.Logger.Warn("metrics: record round", "error", err)
	}
}

// The primary is always a destination; build-only servers are not. Resource
// identity and membership come from trusted persisted placement, not host labels.
func (h *HistoryStore) applicationDestinations(ctx context.Context, id string) ([]string, error) {
	var primary, raw string
	err := h.DB.QueryRowContext(ctx, `SELECT a.server_id, COALESCE(p.destinations,'[]')
 FROM applications a LEFT JOIN application_placements p ON p.application_id=a.id WHERE a.id=?`, id).Scan(&primary, &raw)
	if err != nil {
		return nil, err
	}
	var destinations []string
	if err := json.Unmarshal([]byte(raw), &destinations); err != nil {
		return nil, err
	}
	unique := map[string]bool{primary: true}
	for _, id := range destinations {
		unique[id] = true
	}
	result := make([]string, 0, len(unique))
	for id := range unique {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}
