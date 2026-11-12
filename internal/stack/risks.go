package stack

import (
	"fmt"
	"sort"
	"strings"
)

func stringList(value any) []string {
	values, _ := value.([]any)
	result := []string{}
	for _, item := range values {
		result = append(result, fmt.Sprint(item))
	}
	return result
}

func bindParts(value any) (source, target string, readOnly bool) {
	switch v := value.(type) {
	case string:
		parts := mountSegments(v)
		if len(parts) < 2 {
			return "", "", false
		}
		source, target = parts[0], parts[1]
		if len(parts) > 2 {
			for _, opt := range strings.Split(parts[2], ",") {
				if opt == "ro" {
					readOnly = true
				}
			}
		}
	case map[string]any:
		source, _ = v["source"].(string)
		target, _ = v["target"].(string)
		readOnly, _ = v["read_only"].(bool)
	}
	return
}

func allowedHostBind(source, target string, ro bool) bool {
	return ro && ((source == "/etc/localtime" && target == source) || (source == "/etc/timezone" && target == source))
}

func (d document) hostPorts() []string {
	result := []string{}
	services, _ := d["services"].(map[string]any)
	for name, definition := range services {
		service := definition.(map[string]any)
		ports, _ := service["ports"].([]any)
		for _, port := range ports {
			result = append(result, name+": "+fmt.Sprint(port))
		}
	}
	sort.Strings(result)
	return result
}

// Compose variable defaults can contain colons; they are part of the source.
func mountSegments(value string) []string {
	result := []string{}
	start, depth := 0, 0
	for i := 0; i < len(value); i++ {
		if value[i] == '{' && i > 0 && value[i-1] == '$' {
			depth++
		}
		if value[i] == '}' && depth > 0 {
			depth--
		}
		if value[i] == ':' && depth == 0 {
			result = append(result, value[start:i])
			start = i + 1
		}
	}
	return append(result, value[start:])
}

func hostBindSource(value string) string {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		expression := value[2 : len(value)-1]
		if _, fallback, ok := strings.Cut(expression, ":-"); ok {
			return fallback
		}
		if _, fallback, ok := strings.Cut(expression, "-"); ok {
			return fallback
		}
	}
	return value
}
