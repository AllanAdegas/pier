package cluster

import (
	"fmt"
	"sort"
	"strings"

	"pier/internal/kube"
)

// Health values of a Row.
const (
	healthOK          = "ok"
	healthProgressing = "progressing"
	healthError       = "error"
)

// summarize reduces an object to the few facts a listing shows. Kinds without
// a dedicated reading (custom resources included) still get name and age.
func summarize(kind string, object kube.Object) Row {
	row := Row{
		Name:      text(object, "metadata", "name"),
		Namespace: text(object, "metadata", "namespace"),
		Created:   text(object, "metadata", "creationTimestamp"),
	}
	if describe, known := describers[kind]; known {
		describe(object, &row)
	}
	return row
}

var describers = map[string]func(kube.Object, *Row){
	"Pod":                   describePod,
	"Deployment":            describeReplicated,
	"StatefulSet":           describeReplicated,
	"ReplicaSet":            describeReplicated,
	"DaemonSet":             describeDaemonSet,
	"Job":                   describeJob,
	"CronJob":               describeCronJob,
	"Node":                  describeNode,
	"Namespace":             describePhase,
	"PersistentVolume":      describePhase,
	"PersistentVolumeClaim": describeClaim,
	"Service":               describeService,
	"Ingress":               describeIngress,
	"ConfigMap":             describeKeys,
	"Secret":                describeKeys,
	"Event":                 describeEvent,
}

func describePod(object kube.Object, row *Row) {
	phase := text(object, "status", "phase")
	statuses := list(object, "status", "containerStatuses")
	ready, restarts := 0, 0
	waiting := ""
	for _, item := range statuses {
		status, _ := item.(map[string]any)
		if flag, _ := status["ready"].(bool); flag {
			ready++
		}
		restarts += int(number(status, "restartCount"))
		if reason := text(status, "state", "waiting", "reason"); reason != "" {
			waiting = reason
		}
	}
	row.Status = fmt.Sprintf("%s · %d/%d", phase, ready, len(statuses))
	row.Info = text(object, "spec", "nodeName")
	if restarts > 0 {
		row.Info = strings.TrimSpace(fmt.Sprintf("%s · %d reinício(s)", row.Info, restarts))
	}
	switch {
	case waiting != "" && waiting != "ContainerCreating" && waiting != "PodInitializing":
		// CrashLoopBackOff, ImagePullBackOff, ErrImagePull…
		row.Status, row.Health = waiting, healthError
	case phase == "Failed":
		row.Health = healthError
	case phase == "Succeeded":
		row.Status, row.Health = "Concluído", healthOK
	case phase == "Running" && ready == len(statuses) && len(statuses) > 0:
		row.Health = healthOK
	default:
		row.Health = healthProgressing
	}
}

func describeReplicated(object kube.Object, row *Row) {
	desired := 1
	if spec, ok := object["spec"].(map[string]any); ok {
		if _, set := spec["replicas"]; set {
			desired = int(number(spec, "replicas"))
		}
	}
	ready := int(number(object, "status", "readyReplicas"))
	row.Status = fmt.Sprintf("%d/%d prontos", ready, desired)
	row.Info = firstImage(object)
	switch {
	case desired == 0:
		row.Status = "Escalado para zero"
	case ready >= desired:
		row.Health = healthOK
	default:
		row.Health = healthProgressing
	}
}

func describeDaemonSet(object kube.Object, row *Row) {
	desired := int(number(object, "status", "desiredNumberScheduled"))
	ready := int(number(object, "status", "numberReady"))
	row.Status = fmt.Sprintf("%d/%d prontos", ready, desired)
	row.Info = firstImage(object)
	row.Health = healthProgressing
	if ready >= desired {
		row.Health = healthOK
	}
}

func describeJob(object kube.Object, row *Row) {
	row.Info = firstImage(object)
	switch {
	case number(object, "status", "succeeded") > 0:
		row.Status, row.Health = "Concluído", healthOK
	case number(object, "status", "failed") > 0:
		row.Status, row.Health = "Falhou", healthError
	default:
		row.Status, row.Health = "Em execução", healthProgressing
	}
}

func describeCronJob(object kube.Object, row *Row) {
	row.Info = text(object, "spec", "schedule")
	row.Status = "Ativo"
	if spec, ok := object["spec"].(map[string]any); ok {
		if suspended, _ := spec["suspend"].(bool); suspended {
			row.Status = "Suspenso"
		}
	}
}

func describeNode(object kube.Object, row *Row) {
	row.Status, row.Health = "NotReady", healthError
	for _, item := range list(object, "status", "conditions") {
		condition, _ := item.(map[string]any)
		if condition["type"] == "Ready" && condition["status"] == "True" {
			row.Status, row.Health = "Ready", healthOK
		}
	}
	roles := []string{}
	if labels, ok := dig(object, "metadata", "labels").(map[string]any); ok {
		for label := range labels {
			if role, found := strings.CutPrefix(label, "node-role.kubernetes.io/"); found {
				roles = append(roles, role)
			}
		}
	}
	sort.Strings(roles)
	row.Info = strings.TrimSpace(text(object, "status", "nodeInfo", "kubeletVersion") + " · " + strings.Join(roles, ", "))
}

func describePhase(object kube.Object, row *Row) {
	row.Status = text(object, "status", "phase")
	switch row.Status {
	case "Active", "Bound", "Available":
		row.Health = healthOK
	case "Terminating", "Pending", "Released":
		row.Health = healthProgressing
	case "Failed":
		row.Health = healthError
	}
}

func describeClaim(object kube.Object, row *Row) {
	describePhase(object, row)
	row.Info = strings.Trim(text(object, "status", "capacity", "storage")+" · "+text(object, "spec", "storageClassName"), " ·")
}

func describeService(object kube.Object, row *Row) {
	row.Status = text(object, "spec", "type")
	ports := []string{}
	for _, item := range list(object, "spec", "ports") {
		port, _ := item.(map[string]any)
		ports = append(ports, fmt.Sprintf("%d/%s", int(number(port, "port")), orDefault(text(port, "protocol"), "TCP")))
	}
	row.Info = strings.TrimSpace(text(object, "spec", "clusterIP") + " · " + strings.Join(ports, ", "))
}

func describeIngress(object kube.Object, row *Row) {
	hosts := []string{}
	for _, item := range list(object, "spec", "rules") {
		rule, _ := item.(map[string]any)
		if host := text(rule, "host"); host != "" {
			hosts = append(hosts, host)
		}
	}
	row.Info = strings.Join(hosts, ", ")
	row.Status = text(object, "spec", "ingressClassName")
}

// describeKeys reports how many entries a ConfigMap or Secret holds, never
// their values.
func describeKeys(object kube.Object, row *Row) {
	data, _ := object["data"].(map[string]any)
	row.Info = fmt.Sprintf("%d chave(s)", len(data))
	row.Status = text(object, "type")
}

func describeEvent(object kube.Object, row *Row) {
	row.Status = text(object, "type")
	if row.Status == "Warning" {
		row.Health = healthError
	}
	if last := orDefault(text(object, "lastTimestamp"), text(object, "eventTime")); last != "" {
		row.Created = last
	}
	row.Info = fmt.Sprintf("%s %s/%s: %s", text(object, "reason"), text(object, "involvedObject", "kind"),
		text(object, "involvedObject", "name"), text(object, "message"))
}

func firstImage(object kube.Object) string {
	containers := list(object, "spec", "template", "spec", "containers")
	if len(containers) == 0 {
		containers = list(object, "spec", "jobTemplate", "spec", "template", "spec", "containers")
	}
	if len(containers) == 0 {
		return ""
	}
	container, _ := containers[0].(map[string]any)
	return text(container, "image")
}

// dig walks nested maps; a missing step yields nil.
func dig(object map[string]any, path ...string) any {
	var current any = object
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[key]
	}
	return current
}

func text(object map[string]any, path ...string) string {
	value, _ := dig(object, path...).(string)
	return value
}

func number(object map[string]any, path ...string) float64 {
	value, _ := dig(object, path...).(float64)
	return value
}

func list(object map[string]any, path ...string) []any {
	value, _ := dig(object, path...).([]any)
	return value
}
