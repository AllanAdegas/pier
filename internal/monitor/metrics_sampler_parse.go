package monitor

import (
	"math"
	"strconv"
	"strings"

	"pier/internal/server"
)

var byteUnits = map[string]float64{
	"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
	"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
}

// parsePercent reads docker's "12.34%".
func parsePercent(text string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(text), "%"), 64)
	return value, err == nil && finiteNonNegative(value)
}

// parseMemoryUsage reads the used part of docker's "10.5MiB / 1.9GiB".
func parseMemoryUsage(text string) (int64, bool) {
	used, _, _ := strings.Cut(text, "/")
	used = strings.TrimSpace(used)
	split := strings.IndexFunc(used, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if split <= 0 {
		return 0, false
	}
	number, err := strconv.ParseFloat(used[:split], 64)
	unit, known := byteUnits[strings.TrimSpace(used[split:])]
	if err != nil || !known || !finiteNonNegative(number) || number*unit > math.MaxInt64/2 {
		return 0, false
	}
	return int64(number * unit), true
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// hostSample extracts what the host chart needs from a metrics snapshot.
func hostSample(m server.Metrics) Sample {
	sample := Sample{CPU: m.CPUPercent, MemUsed: m.MemoryUsedBytes}
	if m.MemoryTotalBytes > 0 && m.MemoryUsedBytes != nil {
		total := m.MemoryTotalBytes
		sample.MemTotal = &total
	}
	if m.DiskError == "" && m.DiskTotalBytes > 0 {
		used, total := m.DiskUsedBytes, m.DiskTotalBytes
		sample.DiskUsed, sample.DiskTotal = &used, &total
	}
	if len(m.LoadAverage) > 0 {
		load := m.LoadAverage[0]
		sample.Load1 = &load
	}
	return sample
}

// resourceSamples sums the running containers of each managed resource, so a
// stack with several services has one line. Stopped containers leave a gap.
func resourceSamples(containers []server.ContainerUsage) map[string]Sample {
	samples := map[string]Sample{}
	for _, c := range containers {
		if c.ResourceID == "" || c.State != "running" {
			continue
		}
		cpu, cpuOK := parsePercent(c.CPUPercent)
		memory, memoryOK := parseMemoryUsage(c.MemoryUsage)
		if !cpuOK && !memoryOK {
			continue
		}
		key := SubjectKey(c.ResourceType, c.ResourceID)
		sample := samples[key]
		if cpuOK {
			total := cpu
			if sample.CPU != nil {
				total += *sample.CPU
			}
			sample.CPU = &total
		}
		if memoryOK {
			total := memory
			if sample.MemUsed != nil {
				total += *sample.MemUsed
			}
			sample.MemUsed = &total
		}
		samples[key] = sample
	}
	return samples
}
