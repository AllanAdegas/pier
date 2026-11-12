package diagnose

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// imageInspection is the part of `docker image inspect` output used here.
type imageInspection struct {
	Config struct {
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
	} `json:"Config"`
}

// ExposedPorts returns the sorted, unique port numbers declared with EXPOSE in
// the output of `docker image inspect` (a JSON array). Malformed input or
// entries with an invalid port yield nothing for that part.
func ExposedPorts(imageInspectJSON []byte) []int {
	var images []imageInspection
	if err := json.Unmarshal(imageInspectJSON, &images); err != nil {
		return nil
	}
	seen := map[int]bool{}
	for _, image := range images {
		for key := range image.Config.ExposedPorts {
			if port, ok := parseExposedPort(key); ok {
				seen[port] = true
			}
		}
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	slices.Sort(ports)
	return ports
}

// parseExposedPort reads a key such as "8080/tcp".
func parseExposedPort(key string) (int, bool) {
	number, _, _ := strings.Cut(key, "/")
	port, err := strconv.Atoi(number)
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}
