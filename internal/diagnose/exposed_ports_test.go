package diagnose

import (
	"slices"
	"testing"
)

func TestExposedPorts(t *testing.T) {
	tests := []struct {
		name string
		json string
		want []int
	}{
		{
			name: "tcp and udp ports, sorted and unique",
			json: `[{"Id":"sha256:abc","Config":{"ExposedPorts":{"8080/tcp":{},"53/udp":{},"3000/tcp":{},"8080/udp":{}}}}]`,
			want: []int{53, 3000, 8080},
		},
		{
			name: "port without protocol",
			json: `[{"Config":{"ExposedPorts":{"5432":{}}}}]`,
			want: []int{5432},
		},
		{
			name: "image without EXPOSE",
			json: `[{"Config":{"Image":"app"}}]`,
			want: nil,
		},
		{
			name: "null ExposedPorts",
			json: `[{"Config":{"ExposedPorts":null}}]`,
			want: nil,
		},
		{
			name: "invalid port numbers are dropped",
			json: `[{"Config":{"ExposedPorts":{"0/tcp":{},"70000/tcp":{},"abc/tcp":{},"/tcp":{},"443/tcp":{}}}}]`,
			want: []int{443},
		},
		{
			name: "several images are merged",
			json: `[{"Config":{"ExposedPorts":{"80/tcp":{}}}},{"Config":{"ExposedPorts":{"443/tcp":{},"80/tcp":{}}}}]`,
			want: []int{80, 443},
		},
		{
			name: "empty array",
			json: `[]`,
			want: nil,
		},
		{
			name: "malformed JSON yields nothing",
			json: `[{"Config":`,
			want: nil,
		},
		{
			name: "object instead of array yields nothing",
			json: `{"Config":{"ExposedPorts":{"80/tcp":{}}}}`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExposedPorts([]byte(tt.json))
			if !slices.Equal(got, tt.want) {
				t.Errorf("ExposedPorts = %v, want %v", got, tt.want)
			}
		})
	}
}
