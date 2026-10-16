package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nibinrj/buildlens/internal/quarantine"
)

func TestLoad(t *testing.T) {
	const (
		dbURL = "postgres://u:p@localhost:5432/buildlens"
		key   = "0123456789abcdef-test-key"
	)

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string // substring of the error; empty means no error expected
	}{
		{
			name: "defaults with only the required variables",
			env:  map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_INGEST_KEY": key},
			want: Config{
				HTTPAddr:        ":8080",
				DatabaseURL:     dbURL,
				ShutdownTimeout: 15 * time.Second,
				LogLevel:        slog.LevelInfo,
				MigrateOnStart:  true,
				IngestKey:       key,
				MaxUploadBytes:  64 << 20,
				P3:              quarantine.Defaults(),
			},
		},
		{
			name: "every variable set",
			env: map[string]string{
				"BUILDLENS_DATABASE_URL":     dbURL,
				"BUILDLENS_HTTP_ADDR":        "127.0.0.1:9000",
				"BUILDLENS_SHUTDOWN_TIMEOUT": "3s",
				"BUILDLENS_LOG_LEVEL":        "DEBUG",
				"BUILDLENS_MIGRATE_ON_START": "false",
				"BUILDLENS_INGEST_KEY":       key,
				"BUILDLENS_MAX_UPLOAD_BYTES": "2097152",
			},
			want: Config{
				HTTPAddr:        "127.0.0.1:9000",
				DatabaseURL:     dbURL,
				ShutdownTimeout: 3 * time.Second,
				LogLevel:        slog.LevelDebug,
				MigrateOnStart:  false,
				IngestKey:       key,
				MaxUploadBytes:  2 << 20,
				P3:              quarantine.Defaults(),
			},
		},
		{
			name:    "missing ingest key",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL},
			wantErr: "BUILDLENS_INGEST_KEY is required",
		},
		{
			name:    "ingest key too short",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_INGEST_KEY": "changeme"},
			wantErr: "at least 16 characters",
		},
		{
			name:    "upload limit not a number",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_INGEST_KEY": key, "BUILDLENS_MAX_UPLOAD_BYTES": "64MB"},
			wantErr: "BUILDLENS_MAX_UPLOAD_BYTES",
		},
		{
			name:    "upload limit below 1 MiB",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_INGEST_KEY": key, "BUILDLENS_MAX_UPLOAD_BYTES": "1000"},
			wantErr: "at least 1 MiB",
		},
		{
			name:    "missing database URL",
			env:     map[string]string{},
			wantErr: "BUILDLENS_DATABASE_URL is required",
		},
		{
			name:    "unparseable shutdown timeout",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_SHUTDOWN_TIMEOUT": "soon"},
			wantErr: "BUILDLENS_SHUTDOWN_TIMEOUT",
		},
		{
			name:    "zero shutdown timeout",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_SHUTDOWN_TIMEOUT": "0s"},
			wantErr: "must be positive",
		},
		{
			name:    "unknown log level",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_INGEST_KEY": key, "BUILDLENS_LOG_LEVEL": "verbose"},
			wantErr: "BUILDLENS_LOG_LEVEL",
		},
		{
			name:    "migrate flag not a boolean",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_INGEST_KEY": key, "BUILDLENS_MIGRATE_ON_START": "maybe"},
			wantErr: "BUILDLENS_MIGRATE_ON_START",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Load(func(k string) string { return tc.env[k] })

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Load() error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Load() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRulesFile(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		bad        bool
	}{
		{"partial overrides", "rules:\n  r1_flaky_builds: 3\nrelease_passes: 4\n", false},
		{"unknown root", "release_passess: 4\n", true},
		{"unknown nested", "rules:\n  r4: 2\n", true},
		{"invalid threshold", "rules:\n  r1_flaky_builds: 31\n", true},
		{"multiple documents", "release_passes: 4\n---\nrelease_passes: 5\n", true},
		{"malformed", "rules: [", true},
		{"empty", "", true},
		{"oversized", "release_passes: 4\n#" + strings.Repeat("x", 1<<20), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rules.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(func(k string) string {
				switch k {
				case "BUILDLENS_DATABASE_URL":
					return "postgres://localhost/test"
				case "BUILDLENS_INGEST_KEY":
					return "test-key-0123456789"
				case "BUILDLENS_RULES_FILE":
					return path
				}
				return ""
			})
			if (err != nil) != tc.bad {
				t.Fatalf("Load error=%v, want bad=%v", err, tc.bad)
			}
			if !tc.bad && (cfg.P3.ReleasePasses != 4 || cfg.P3.Rules.R1FlakyBuilds != 3 || cfg.P3.CapMaxTests != 10) {
				t.Fatalf("overrides/defaults=%+v", cfg.P3)
			}
		})
	}
	cfg := quarantine.Defaults()
	if err := loadPolicy(filepath.Join(t.TempDir(), "missing"), &cfg); err == nil {
		t.Fatal("missing policy accepted")
	}
}
