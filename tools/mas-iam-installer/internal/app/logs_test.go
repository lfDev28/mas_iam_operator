package app

import (
	"strings"
	"testing"
)

func TestLogsComponentArgs(t *testing.T) {
	tests := []struct {
		name      string
		opts      logsOptions
		want      string
		wantError bool
	}{
		{
			name: "bridge deployment",
			opts: logsOptions{component: "bridge", tail: 200},
			want: "deployment/scim-bridge --tail=200",
		},
		{
			name: "install job reattach",
			opts: logsOptions{component: "install-job", jobName: installerJobDefaultName, tail: 200, follow: true},
			want: "job/mas-est-install --tail=200 -f",
		},
		{
			name: "install job honours a custom job name",
			opts: logsOptions{component: "install-job", jobName: "mas-est-install-lee", tail: 50},
			want: "job/mas-est-install-lee --tail=50",
		},
		{
			name:      "unknown component",
			opts:      logsOptions{component: "nope"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tt.opts.componentArgs()
			if tt.wantError {
				if err == nil {
					t.Fatalf("componentArgs() = %v, want error", args)
				}
				return
			}
			if err != nil {
				t.Fatalf("componentArgs() error = %v", err)
			}
			if got := strings.Join(args, " "); got != tt.want {
				t.Fatalf("componentArgs() = %q, want %q", got, tt.want)
			}
		})
	}
}
