package main

import (
	"io"
	"testing"
	"time"
)

func TestAgentBusyMetadata(t *testing.T) {
	for _, tc := range []struct {
		busy    string
		wantErr bool
	}{{"", false}, {"0", false}, {"1000", false}, {"-1", true}, {"1001", true}, {"invalid", true}} {
		t.Run(tc.busy, func(t *testing.T) {
			args := []string{"--server", "http://localhost:8091", "--key", "test-key", "--repo", "owner/lab", "--job", "lab/main", "--build-number", "1", "--branch", "main", "--commit", "0123456789abcdef", "--result", "SUCCESS", "--started-at", "1792152000000", "--duration-ms", "1000"}
			if tc.busy != "" {
				args = append(args, "--agent-busy-ms", tc.busy)
			}
			f, err := parseReportFlags(args, func(string) string { return "" }, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			m, err := buildMetadata(f, time.Now())
			if (err != nil) != tc.wantErr {
				t.Fatalf("%+v %v", m.Agent, err)
			}
			if err == nil && (m.Agent.BusyMs == nil) != (tc.busy == "") {
				t.Fatal("missing busy duration became known")
			}
		})
	}
}
