package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	const dbURL = "postgres://u:p@localhost:5432/buildlens"

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string // substring of the error; empty means no error expected
	}{
		{
			name: "defaults with only the database URL",
			env:  map[string]string{"BUILDLENS_DATABASE_URL": dbURL},
			want: Config{
				HTTPAddr:        ":8080",
				DatabaseURL:     dbURL,
				ShutdownTimeout: 15 * time.Second,
				LogLevel:        slog.LevelInfo,
				MigrateOnStart:  true,
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
			},
			want: Config{
				HTTPAddr:        "127.0.0.1:9000",
				DatabaseURL:     dbURL,
				ShutdownTimeout: 3 * time.Second,
				LogLevel:        slog.LevelDebug,
				MigrateOnStart:  false,
			},
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
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_LOG_LEVEL": "verbose"},
			wantErr: "BUILDLENS_LOG_LEVEL",
		},
		{
			name:    "migrate flag not a boolean",
			env:     map[string]string{"BUILDLENS_DATABASE_URL": dbURL, "BUILDLENS_MIGRATE_ON_START": "maybe"},
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
