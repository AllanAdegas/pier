package memo

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentCallsShareOneLoad(t *testing.T) {
	group := New[int](time.Minute, time.Second)
	var loads atomic.Int32
	release := make(chan struct{})
	load := func(context.Context) (int, error) {
		loads.Add(1)
		<-release
		return 42, nil
	}
	var wg sync.WaitGroup
	results := make([]int, 50)
	for index := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index], _ = group.Do(context.Background(), "srv1", load)
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if loads.Load() != 1 {
		t.Errorf("50 concurrent calls ran %d loads; they must share one", loads.Load())
	}
	for _, result := range results {
		if result != 42 {
			t.Fatalf("every caller gets the shared result, got %v", results)
		}
	}
}

func TestResultsExpireAndFailuresAreNotKept(t *testing.T) {
	group := New[int](time.Second, time.Second)
	clock := time.Now()
	group.now = func() time.Time { return clock }
	var loads atomic.Int32
	load := func(context.Context) (int, error) { return int(loads.Add(1)), nil }

	first, _ := group.Do(context.Background(), "k", load)
	again, _ := group.Do(context.Background(), "k", load)
	if first != 1 || again != 1 {
		t.Errorf("within the ttl the result is reused: %d, %d", first, again)
	}
	clock = clock.Add(2 * time.Second)
	if later, _ := group.Do(context.Background(), "k", load); later != 2 {
		t.Errorf("after the ttl it loads again, got %d", later)
	}
	if other, _ := group.Do(context.Background(), "other", load); other != 3 {
		t.Errorf("keys are independent, got %d", other)
	}

	boom := errors.New("boom")
	_, err := group.Do(context.Background(), "failing", func(context.Context) (int, error) { return 0, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("the error reaches the caller: %v", err)
	}
	if value, err := group.Do(context.Background(), "failing", load); err != nil || value != 4 {
		t.Errorf("a failure is not cached: %d %v", value, err)
	}
}

func TestACancelledCallerDoesNotCancelTheSharedLoad(t *testing.T) {
	group := New[string](time.Minute, time.Second)
	release := make(chan struct{})
	load := func(ctx context.Context) (string, error) {
		<-release
		return "ok", ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := group.Do(ctx, "k", load); !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled caller returns: %v", err)
	}
	close(release)
	if value, err := group.Do(context.Background(), "k", load); err != nil || value != "ok" {
		t.Errorf("the load kept going for the others: %q %v", value, err)
	}
}
