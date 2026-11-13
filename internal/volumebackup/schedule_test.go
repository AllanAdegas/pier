package volumebackup

import (
	"context"
	"testing"
	"time"

	"pier/internal/notify"
)

type recordedObservation struct {
	event  notify.Event
	active bool
}

type fakeObserver struct{ seen []recordedObservation }

func (o *fakeObserver) Observe(_ context.Context, _ notify.Publisher, event notify.Event, active bool, _ time.Time) error {
	o.seen = append(o.seen, recordedObservation{event, active})
	return nil
}

func TestMissedScheduleUsesTheCronAndATenMinuteGrace(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(2026, 1, 5, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		now  time.Time
		last *time.Time
		want bool
	}{
		{time.Date(2026, 1, 6, 3, 5, 0, 0, time.UTC), &last, false},
		{time.Date(2026, 1, 6, 3, 11, 0, 0, time.UTC), &last, true},
		{time.Date(2026, 1, 1, 3, 11, 0, 0, time.UTC), nil, true},
		{time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC), nil, false},
	}
	for i, c := range cases {
		if got := missedSchedule("0 3 * * *", created, c.last, c.now); got != c.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
	if missedSchedule("garbage", created, nil, time.Now()) {
		t.Error("an invalid cron never reports a miss")
	}
}

func TestObserveMissedRaisesBackupMissedWithTheResourceType(t *testing.T) {
	f := newFixture(t)
	observer := &fakeObserver{}
	f.service.SetAlertObserver(observer)
	ctx := context.Background()
	_, _ = f.service.SetPolicy(ctx, KindApplication, "app1", Policy{Enabled: true, Cron: "0 3 * * *"})
	f.service.ObserveMissed(ctx, time.Now().Add(72*time.Hour))
	if len(observer.seen) != 1 {
		t.Fatalf("observations: %+v", observer.seen)
	}
	seen := observer.seen[0]
	if seen.event.Type != notify.BackupMissed || seen.event.ResourceType != KindApplication || seen.event.Resource != "app1" || !seen.active {
		t.Errorf("observation: %+v", seen)
	}
	f.backupAndWait(t)
	observer.seen = nil
	f.service.ObserveMissed(ctx, time.Now())
	if len(observer.seen) != 1 || observer.seen[0].active {
		t.Errorf("a fresh backup must clear the alert: %+v", observer.seen)
	}
}

func TestDisabledPoliciesAreNotObserved(t *testing.T) {
	f := newFixture(t)
	observer := &fakeObserver{}
	f.service.SetAlertObserver(observer)
	_, _ = f.service.SetPolicy(context.Background(), KindApplication, "app1", Policy{Enabled: false})
	f.service.ObserveMissed(context.Background(), time.Now().Add(1000*time.Hour))
	if len(observer.seen) != 0 {
		t.Errorf("observations: %+v", observer.seen)
	}
}
