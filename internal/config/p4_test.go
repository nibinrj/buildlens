package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalysisConfig(t *testing.T) {
	for _, tc := range []struct {
		name, token, repos, policy string
		wantErr                    bool
	}{
		{"enabled", "test-token", "owner/lab", "regression:\n  window: 12\n  min_samples: 10\n", false},
		{"missing allowlist", "test-token", "", "", true},
		{"invalid repo", "test-token", "owner/../lab", "", true},
		{"empty list member", "test-token", "owner/lab,", "", true},
		{"impossible baseline", "", "", "regression:\n  window: 4\n  min_samples: 10\n", true},
		{"nan", "", "", "regression:\n  mad_multiplier: .nan\n", true},
		{"unknown threshold", "", "", "regression:\n  unexpected: 1\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"BUILDLENS_DATABASE_URL": "postgres://test", "BUILDLENS_INGEST_KEY": strings.Repeat("x", 32), "BUILDLENS_GITHUB_TOKEN": tc.token, "BUILDLENS_GITHUB_REPOSITORIES": tc.repos}
			if tc.policy != "" {
				path := filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(path, []byte(tc.policy), 0600); err != nil {
					t.Fatal(err)
				}
				env["BUILDLENS_RULES_FILE"] = path
			}
			cfg, err := Load(func(k string) string { return env[k] })
			if (err != nil) != tc.wantErr {
				t.Fatalf("%+v %v", cfg.Regression, err)
			}
			if err != nil && strings.Contains(err.Error(), "test-token") {
				t.Fatal("token leaked")
			}
		})
	}
}
