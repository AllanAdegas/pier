package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"pier/internal/server"
)

type destinationSource struct{ failed bool }

func (d destinationSource) Metrics(_ context.Context, id string) (server.Metrics, error) {
	if d.failed && id == "b" {
		return server.Metrics{}, errors.New("offline")
	}
	cpu, mem := "10%", "100B / 1KiB"
	if id == "b" {
		cpu, mem = "20%", "200B / 1KiB"
	}
	return server.Metrics{CPUPercent: f(7), Containers: []server.ContainerUsage{
		{ResourceType: "application", ResourceID: "app", State: "running", CPUPercent: cpu, MemoryUsage: mem},
		{ResourceType: "application", ResourceID: "foreign", State: "running", CPUPercent: "99%", MemoryUsage: "999B / 1KiB"},
	}}, nil
}

func TestSamplerDestinationTotalsIndependentOfOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "forward", true: "reverse"}[reverse], func(t *testing.T) {
			servers := fakeServers{{ID: "a", Status: server.StatusReachable}, {ID: "b", Status: server.StatusReachable}}
			if reverse {
				servers[0], servers[1] = servers[1], servers[0]
			}
			s := testSampler(t, servers, destinationSource{})
			for _, sql := range []string{
				`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('a','a','local','/tmp','x'),('b','b','local','/tmp','x'),('c','c','local','/tmp','x')`,
				`INSERT INTO projects(id,name,created_at) VALUES('p','p','x'),('q','q','x')`,
				`INSERT INTO environments(id,project_id,name,created_at) VALUES('e','p','e','x'),('f','q','f','x')`,
				`INSERT INTO applications(id,environment_id,server_id,name,config,webhook_secret,created_at,updated_at) VALUES('app','e','a','app','{}','x','x','x'),('foreign','f','c','foreign','{}','x','x','x')`,
				`INSERT INTO application_placements(application_id,destinations,updated_at) VALUES('app','["b","a","b"]','x')`,
			} {
				if _, err := s.Store.DB.Exec(sql); err != nil {
					t.Fatal(err)
				}
			}
			s.SampleOnce(context.Background())
			check := func(subject string, want float64) {
				t.Helper()
				r, _ := ParseRange("1h")
				h, err := s.Store.Query(context.Background(), subject, r, bucketStart.Add(2*time.Minute))
				if err != nil || len(h.Series.T) != 1 || *h.Series.CPU[0] != want {
					t.Fatalf("%s: %+v %v", subject, h, err)
				}
			}
			check("application:app", 30)
			check("server:a", 7)
			check("server:b", 7)
			r, _ := ParseRange("1h")
			h, _ := s.Store.Query(context.Background(), "application:app", r, bucketStart.Add(2*time.Minute))
			if *h.Series.MemUsed[0] != 300 {
				t.Fatal("memory total", h)
			}
			var n int
			if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM metric_points_raw WHERE subject LIKE 'application_destination:%'`).Scan(&n); err != nil || n != 2 {
				t.Fatal("destination samples", n, err)
			}
			h, _ = s.Store.Query(context.Background(), "application:foreign", r, bucketStart.Add(2*time.Minute))
			if len(h.Series.T) != 0 {
				t.Fatal("untrusted cross-project label accepted")
			}
			s.Source = destinationSource{failed: true}
			s.SampleOnce(context.Background()) // retry in the same minute must clear the total
			h, _ = s.Store.Query(context.Background(), "application:app", r, bucketStart.Add(2*time.Minute))
			if len(h.Series.T) != 0 {
				t.Fatal("stale total after partial retry", h)
			}
			s.Source = destinationSource{}
			s.SampleOnce(context.Background())
			s.Now = func() time.Time { return bucketStart.Add(2 * time.Minute) }
			s.Source = destinationSource{failed: true}
			s.SampleOnce(context.Background())
			h, _ = s.Store.Query(context.Background(), "application:app", r, bucketStart.Add(3*time.Minute))
			if len(h.Series.T) != 1 {
				t.Fatal("partial collection must leave gap", h)
			}
			if err := s.Store.Compact(context.Background(), bucketStart.Add(16*time.Minute)); err != nil {
				t.Fatal(err)
			}
			r, _ = ParseRange("7d")
			h, _ = s.Store.Query(context.Background(), "application:app", r, bucketStart.Add(17*time.Minute))
			if len(h.Series.T) != 1 || *h.Series.CPU[0] != 30 || *h.Series.MemUsed[0] != 300 {
				t.Fatal("compacted totals", h)
			}
		})
	}
}
