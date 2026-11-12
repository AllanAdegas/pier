package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"pier/internal/platform/executor"
)

const hostMetricsCommand = `set -e
test -r /proc/stat && test -r /proc/meminfo && test -r /proc/uptime && test -r /proc/loadavg
printf 'CPU_START\n'
head -n 1 /proc/stat
sleep 0.25
printf 'CPU_END\n'
head -n 1 /proc/stat
printf 'MEMORY\n'
cat /proc/meminfo
printf 'UPTIME\n'
cat /proc/uptime
printf 'LOAD\n'
cat /proc/loadavg`

func readHostMetrics(ctx context.Context, ex executor.Executor, metrics *Metrics) error {
	output, err := ex.Run(ctx, hostMetricsCommand)
	if err != nil {
		return err
	}
	return parseHostMetrics(output, metrics)
}

func parseHostMetrics(output string, metrics *Metrics) error {
	sections := make(map[string]string)
	section := ""
	for _, line := range strings.Split(output, "\n") {
		switch line {
		case "CPU_START", "CPU_END", "MEMORY", "UPTIME", "LOAD":
			section = line
		default:
			sections[section] += line + "\n"
		}
	}
	var failures []error
	cpu, err := cpuUsage(sections["CPU_START"], sections["CPU_END"])
	if err != nil {
		failures = append(failures, err)
	} else {
		metrics.CPUPercent = &cpu
	}
	if err := parseMemory(sections["MEMORY"], metrics); err != nil {
		failures = append(failures, err)
	}
	uptime := strings.Fields(sections["UPTIME"])
	if len(uptime) == 0 {
		failures = append(failures, fmt.Errorf("missing uptime"))
	} else if value, err := positiveFloat(uptime[0]); err != nil {
		failures = append(failures, err)
	} else {
		metrics.UptimeSeconds = &value
	}
	load := strings.Fields(sections["LOAD"])
	if len(load) < 3 {
		failures = append(failures, fmt.Errorf("missing load averages"))
	} else {
		values := make([]float64, 3)
		for index := range values {
			values[index], err = positiveFloat(load[index])
			if err != nil {
				failures = append(failures, err)
				break
			}
		}
		if err == nil {
			metrics.LoadAverage = values
		}
	}
	return errors.Join(failures...)
}

func cpuUsage(before, after string) (float64, error) {
	first, last := strings.Fields(before), strings.Fields(after)
	if len(first) < 5 || len(last) < 5 || first[0] != "cpu" || last[0] != "cpu" || len(first) != len(last) {
		return 0, fmt.Errorf("invalid CPU counters")
	}
	var total, idle int64
	for index := 1; index < len(first) && index <= 8; index++ {
		start, startErr := strconv.ParseInt(first[index], 10, 64)
		end, endErr := strconv.ParseInt(last[index], 10, 64)
		if startErr != nil || endErr != nil || start < 0 || end < start {
			return 0, fmt.Errorf("invalid CPU counter delta")
		}
		delta := end - start
		total += delta
		if index == 4 || index == 5 {
			idle += delta
		}
	}
	if total <= 0 || idle > total {
		return 0, fmt.Errorf("empty CPU sample")
	}
	return math.Round(float64(total-idle)/float64(total)*1000) / 10, nil
}

func parseMemory(output string, metrics *Metrics) error {
	values := make(map[string]int64)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err == nil && value >= 0 && value <= math.MaxInt64/1024 {
			values[strings.TrimSuffix(fields[0], ":")] = value * 1024
		}
	}
	total, hasTotal := values["MemTotal"]
	available, hasAvailable := values["MemAvailable"]
	if !hasTotal || !hasAvailable || total <= 0 || available > total {
		return fmt.Errorf("invalid RAM counters")
	}
	used := total - available
	metrics.MemoryTotalBytes, metrics.MemoryUsedBytes, metrics.MemoryAvailableBytes = total, &used, &available
	swapTotal, hasSwapTotal := values["SwapTotal"]
	swapFree, hasSwapFree := values["SwapFree"]
	if !hasSwapTotal || !hasSwapFree || swapFree > swapTotal {
		return fmt.Errorf("invalid swap counters")
	}
	swapUsed := swapTotal - swapFree
	metrics.SwapTotalBytes, metrics.SwapUsedBytes = &swapTotal, &swapUsed
	return nil
}

func positiveFloat(input string) (float64, error) {
	value, err := strconv.ParseFloat(input, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, fmt.Errorf("invalid host counter")
	}
	return value, nil
}
