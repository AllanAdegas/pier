package monitor

import (
	"context"
	"encoding/json"
	"time"

	"pier/internal/docker"
	"pier/internal/notify"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

type restartSample struct {
	at    time.Time
	count int
}

func (m *Monitor) observe(ctx context.Context, kind notify.EventType, resource, message string, active bool) {
	if m.Alerts == nil {
		return
	}
	event := notify.Event{Type: kind, Resource: resource, Title: i18n.Instance().Text("Alerta de execução"), Message: message}
	if err := m.Alerts.Observe(ctx, m.Events, event, active, time.Now()); err != nil {
		m.Logger.Warn("monitor: alert state", "error", err)
	}
}

func (m *Monitor) runtimeAlerts(ctx context.Context, target server.Server, containers []docker.Container) {
	if m.Alerts == nil {
		return
	}
	ex, _, err := m.Connector.Executor(ctx, target.ID)
	if err != nil {
		return
	}
	// Docker RestartCount lives at the top level of inspect, not under State.
	type inspection struct {
		ID           string
		RestartCount int
		State        struct {
			OOMKilled bool
			ExitCode  int
			Health    struct{ Status string }
		}
	}
	if m.restarts == nil {
		m.restarts = map[string][]restartSample{}
	}
	conditions := map[string]map[notify.EventType]bool{}
	now := time.Now()
	for id, samples := range m.restarts {
		if len(samples) == 0 || now.Sub(samples[len(samples)-1].at) > 10*time.Minute {
			delete(m.restarts, id)
		}
	}
	for _, c := range containers {
		if c.ResourceID == "" {
			continue
		}
		output, err := ex.Run(ctx, shell.Join("docker", "inspect", c.ID))
		if err != nil {
			continue
		}
		var values []inspection
		if json.Unmarshal([]byte(output), &values) != nil || len(values) != 1 {
			continue
		}
		v := values[0]
		samples, loop := restartWindow(m.restarts[c.ID], now, v.RestartCount, m.Interval)
		m.restarts[c.ID] = samples
		unhealthy := v.State.Health.Status == "unhealthy"
		if c.Type == docker.TypeApplication {
			d, e := m.Applications.GetDeployment(ctx, c.Deployment)
			unhealthy = unhealthy && e == nil && d.Status == "finished"
		}
		if conditions[c.ResourceID] == nil {
			conditions[c.ResourceID] = map[notify.EventType]bool{}
		}
		for k, active := range map[notify.EventType]bool{notify.ContainerRestarting: loop, notify.ContainerOOMKilled: v.State.OOMKilled || v.State.ExitCode == 137, notify.ContainerUnhealthy: unhealthy} {
			conditions[c.ResourceID][k] = conditions[c.ResourceID][k] || active
		}
	}
	messages := map[notify.EventType]string{
		notify.ContainerRestarting: i18n.Instance().Text("O contêiner reiniciou ao menos 3 vezes em 10 minutos. Confira os logs e o comando de início."),
		notify.ContainerOOMKilled:  i18n.Instance().Text("O contêiner ficou sem memória. Aumente o limite de memória ou a capacidade do servidor."),
		notify.ContainerUnhealthy:  i18n.Instance().Text("A saúde do contêiner falhou. Confira a porta, a rota de saúde e os logs."),
	}
	for resource, states := range conditions {
		for kind, active := range states {
			m.observe(ctx, kind, resource, messages[kind], active)
		}
	}
	m.thresholdAlerts(ctx, target)
}

func (m *Monitor) thresholdAlerts(ctx context.Context, target server.Server) {
	metrics, err := m.Servers.Metrics(ctx, target.ID)
	if err != nil {
		m.resetThresholds(target.ID)
		return
	}
	options, err := m.Alerts.Options(ctx)
	if err != nil {
		return
	}
	if m.highSince == nil {
		m.highSince = map[string]time.Time{}
	}
	check := func(key string, value *float64, threshold float64, kind notify.EventType) {
		resource := target.ID + ":" + key
		if value == nil {
			delete(m.highSince, resource)
			return
		}
		high := *value > threshold
		since, sustained := sustainedThreshold(m.highSince[resource], time.Now(), high, time.Duration(options.Minutes)*time.Minute)
		if since.IsZero() {
			delete(m.highSince, resource)
		} else {
			m.highSince[resource] = since
		}
		if !high || sustained {
			m.observe(ctx, kind, resource, i18n.Instance().Sprintf("Uso de %s acima de %.0f%%. Libere espaço ou aumente a capacidade do servidor.", key, threshold), sustained)
		}
	}
	check("CPU", metrics.CPUPercent, options.CPU, notify.ServerResourceHigh)
	var memory, disk *float64
	if metrics.MemoryUsedBytes != nil && metrics.MemoryTotalBytes > 0 {
		v := 100 * float64(*metrics.MemoryUsedBytes) / float64(metrics.MemoryTotalBytes)
		memory = &v
	}
	if metrics.DiskError == "" && metrics.DiskTotalBytes > 0 {
		v := 100 * float64(metrics.DiskUsedBytes) / float64(metrics.DiskTotalBytes)
		disk = &v
	}
	check("RAM", memory, options.Memory, notify.ServerResourceHigh)
	check("disco", disk, options.Disk, notify.ServerDiskHigh)
}

// restartWindow measures changes in the cumulative counter, never its lifetime total.
func restartWindow(samples []restartSample, now time.Time, count int, interval time.Duration) ([]restartSample, bool) {
	cutoff := now.Add(-10 * time.Minute)
	for len(samples) > 1 && samples[1].at.Before(cutoff) {
		samples = samples[1:]
	}
	if len(samples) > 0 && count < samples[len(samples)-1].count {
		samples = nil
	}
	loop := len(samples) > 0 && now.Sub(samples[0].at) <= 10*time.Minute+interval && count-samples[0].count >= 3
	return append(samples, restartSample{now, count}), loop
}

func sustainedThreshold(since, now time.Time, high bool, duration time.Duration) (time.Time, bool) {
	if !high {
		return time.Time{}, false
	}
	if since.IsZero() {
		since = now
	}
	return since, now.Sub(since) >= duration
}

func (m *Monitor) resetThresholds(serverID string) {
	for _, key := range []string{"CPU", "RAM", "disco"} {
		delete(m.highSince, serverID+":"+key)
	}
}
