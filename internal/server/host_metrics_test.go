package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pier/internal/platform/executor"
)

const hostFixture = `CPU_START
cpu 100 0 50 700 50 0 0 0 90 0
CPU_END
cpu 130 0 60 740 60 0 0 10 120 0
MEMORY
MemTotal: 1000 kB
MemFree: 100 kB
MemAvailable: 400 kB
SwapTotal: 200 kB
SwapFree: 150 kB
UPTIME
7200.50 22000.00
LOAD
0.25 0.50 0.75 1/100 25`

func TestHostMetricsParseRealUsage(t *testing.T) {
	var metrics Metrics
	if err := parseHostMetrics(hostFixture, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.CPUPercent == nil || *metrics.CPUPercent != 50 {
		t.Fatalf("CPU guest time must not be counted twice: %v", metrics.CPUPercent)
	}
	if metrics.MemoryTotalBytes != 1024000 || *metrics.MemoryUsedBytes != 614400 || *metrics.MemoryAvailableBytes != 409600 {
		t.Fatalf("RAM must use MemAvailable, not MemFree: %+v", metrics)
	}
	if *metrics.SwapUsedBytes != 51200 || *metrics.SwapTotalBytes != 204800 || *metrics.UptimeSeconds != 7200.5 {
		t.Fatalf("wrong swap/uptime: %+v", metrics)
	}
	if len(metrics.LoadAverage) != 3 || metrics.LoadAverage[2] != 0.75 {
		t.Fatalf("wrong load averages: %v", metrics.LoadAverage)
	}
}

func TestHostMetricsZeroSwapIsAvailable(t *testing.T) {
	var metrics Metrics
	fixture := strings.ReplaceAll(strings.ReplaceAll(hostFixture, "SwapTotal: 200", "SwapTotal: 0"), "SwapFree: 150", "SwapFree: 0")
	if err := parseHostMetrics(fixture, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.SwapTotalBytes == nil || *metrics.SwapTotalBytes != 0 || *metrics.SwapUsedBytes != 0 {
		t.Fatal("disabled swap must be reported as zero, not unavailable")
	}
}

func TestHostMetricsUnavailableIsNotZero(t *testing.T) {
	var metrics Metrics
	ex := executor.NewFake().On("/proc/stat", "", 1)
	if err := readHostMetrics(context.Background(), ex, &metrics); err == nil {
		t.Fatal("expected collection error")
	}
	encoded, err := json.Marshal(metrics)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"cpu_percent", "memory_used_bytes", "memory_available_bytes", "swap_used_bytes", "uptime_seconds", "load_average"} {
		if !strings.Contains(string(encoded), `"`+field+`":null`) {
			t.Errorf("missing nullable metric %s: %s", field, encoded)
		}
	}
}

func TestHostMetricsRejectInvalidSamples(t *testing.T) {
	for _, fixture := range []string{
		"", strings.Replace(hostFixture, "cpu 130", "cpu 10", 1),
		strings.Replace(hostFixture, "MemAvailable: 400", "MemAvailable: 1400", 1),
		strings.Replace(hostFixture, "7200.50", "NaN", 1),
		strings.Replace(hostFixture, "0.25 0.50", "0.25 +Inf", 1),
		strings.Replace(hostFixture, "MemAvailable: 400 kB", "MemAvailable: -1 kB", 1),
	} {
		var metrics Metrics
		if err := parseHostMetrics(fixture, &metrics); err == nil {
			t.Errorf("expected malformed sample to fail: %q", fixture)
		}
	}
}

func TestHostMetricsPreservesOtherDataOnPartialFailure(t *testing.T) {
	var metrics Metrics
	fixture := strings.Replace(hostFixture, "MemAvailable: 400 kB", "", 1)
	if err := parseHostMetrics(fixture, &metrics); err == nil {
		t.Fatal("expected incomplete sample")
	}
	if metrics.CPUPercent == nil || metrics.UptimeSeconds == nil || len(metrics.LoadAverage) != 3 || metrics.MemoryUsedBytes != nil {
		t.Fatalf("valid independent metrics should remain available: %+v", metrics)
	}
}

func TestDiskMetricsUseFilesystemAvailableSpace(t *testing.T) {
	ex := executor.NewFake().On("df -Pk", "disk 1000 600 350 64% /data", 0)
	var metrics Metrics
	if err := readDiskUsage(context.Background(), ex, "/data/with 'quotes'", &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.DiskTotalBytes != 1024000 || metrics.DiskUsedBytes != 614400 || *metrics.DiskAvailableBytes != 358400 {
		t.Fatalf("reserved blocks must not be reported as available: %+v", metrics)
	}
	for _, output := range []string{"", "df: no such file", "disk 1000 invalid 350", "disk 1000 600 -1 64% /data"} {
		var missing Metrics
		ex.On("df -Pk", output, 0)
		if err := readDiskUsage(context.Background(), ex, "/missing", &missing); err == nil || missing.DiskAvailableBytes != nil {
			t.Errorf("invalid df response must remain unavailable: %q", output)
		}
	}
}

func TestDockerMetricsCountWholeDaemon(t *testing.T) {
	ex := executor.NewFake().On("info", "Linux\tx86_64\t4\t8000\t7\t15\t10\t2\t1", 0)
	metrics, err := readDockerInfo(context.Background(), ex)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.ContainersTotal != 10 || metrics.ContainersRunning != 7 || metrics.ContainersStopped != 2 || metrics.ContainersPaused != 1 {
		t.Fatalf("wrong container counts: %+v", metrics)
	}
}

func TestManagedContainerMetricsIncludeStoppedWithoutInventingUsage(t *testing.T) {
	ex := executor.NewFake().On("ps", "id1\tweb\tnginx\trunning\tUp 1h\tapp\tapp1\tid\nid2\tdb\tpostgres\texited\tExited (0)\tdatabase\tdb1\tid", 0).
		On("stats", "web\t12.5%\t100MiB / 1GiB\t9.7%\nunmanaged\t90%\t1GiB / 2GiB\t50%", 0)
	containers, err := readContainerUsage(context.Background(), ex)
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 2 || containers[0].CPUPercent != "12.5%" || containers[1].State != "exited" || containers[1].MemoryUsage != "" {
		t.Fatalf("wrong managed usage: %+v", containers)
	}
}
