package diagnose

import (
	"slices"
	"testing"
)

const procNetTCPHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when " +
	"retrnsmt   uid  timeout inode\n"

func TestParseProcNet(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []Listener
	}{
		{
			name: "IPv4 loopback listener",
			content: procNetTCPHeader +
				"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1",
			want: []Listener{{Address: "127.0.0.1", Port: 8080, Loopback: true}},
		},
		{
			name: "IPv4 wildcard listener",
			content: procNetTCPHeader +
				"   0: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 99 1",
			want: []Listener{{Address: "0.0.0.0", Port: 3000, Loopback: false}},
		},
		{
			name: "established sockets are ignored",
			content: procNetTCPHeader +
				"   1: 1F00A8C0:0BB8 0100007F:D2F0 01 00000000:00000000 00:00000000 00000000  1000        0 77 1",
			want: nil,
		},
		{
			name: "IPv6 loopback listener",
			content: "  sl  local_address                         remote_address                        st\n" +
				"   0: 00000000000000000000000001000000:1F90 00000000000000000000000000000000:0000 0A",
			want: []Listener{{Address: "::1", Port: 8080, Loopback: true}},
		},
		{
			name:    "IPv6 wildcard listener",
			content: "   0: 00000000000000000000000000000000:0BB8 00000000000000000000000000000000:0000 0A",
			want:    []Listener{{Address: "::", Port: 3000, Loopback: false}},
		},
		{
			name:    "IPv4-mapped loopback is loopback",
			content: "   0: 0000000000000000FFFF00000100007F:0050 00000000000000000000000000000000:0000 0A",
			want:    []Listener{{Address: "127.0.0.1", Port: 80, Loopback: true}},
		},
		{
			name: "IPv4 and IPv6 tables concatenated",
			content: procNetTCPHeader +
				"   0: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 99 1\n" +
				"   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12 1\n",
			want: []Listener{
				{Address: "0.0.0.0", Port: 3000, Loopback: false},
				{Address: "127.0.0.1", Port: 8080, Loopback: true},
			},
		},
		{
			name: "malformed rows are skipped",
			content: "   0: 0100007F 00000000:0000 0A\n" +
				"   1: ZZ00007F:1F90 00000000:0000 0A\n" +
				"   2: 0100007F:XYZ1 00000000:0000 0A\n" +
				"   3: 0100007F:1F90 00000000:0000 0A\n" +
				"truncated",
			want: []Listener{{Address: "127.0.0.1", Port: 8080, Loopback: true}},
		},
		{
			name:    "address of unexpected length is skipped",
			content: "   0: 0100007F00:1F90 00000000:0000 0A",
			want:    nil,
		},
		{
			name:    "empty content",
			content: "",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseProcNet(tt.content)
			if !slices.Equal(got, tt.want) {
				t.Errorf("ParseProcNet = %+v, want %+v", got, tt.want)
			}
		})
	}
}
