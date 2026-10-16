// Package config reads the server configuration from environment variables.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nibinrj/buildlens/internal/quarantine"
)

// Config is the server configuration. Every field comes from an environment variable.
type Config struct {
	HTTPAddr        string            // BUILDLENS_HTTP_ADDR, default ":8080"
	DatabaseURL     string            // BUILDLENS_DATABASE_URL, required
	ShutdownTimeout time.Duration     // BUILDLENS_SHUTDOWN_TIMEOUT, default 15s
	LogLevel        slog.Level        // BUILDLENS_LOG_LEVEL: debug, info, warn, error; default info
	MigrateOnStart  bool              // BUILDLENS_MIGRATE_ON_START, default true
	IngestKey       string            // BUILDLENS_INGEST_KEY, required, at least MinKeyLength characters; never logged
	MaxUploadBytes  int64             // BUILDLENS_MAX_UPLOAD_BYTES, default 64 MiB: the largest POST /api/v1/builds body
	P3              quarantine.Config // BUILDLENS_RULES_FILE optional strict YAML; otherwise defaults
}

// MinKeyLength is the shortest API key the server accepts, so a placeholder like "changeme" fails at start.
const MinKeyLength = 16

// Load builds a Config from getenv. Pass os.Getenv in production and a map lookup in tests.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:        ":8080",
		ShutdownTimeout: 15 * time.Second,
		LogLevel:        slog.LevelInfo,
		MigrateOnStart:  true,
		MaxUploadBytes:  64 << 20,
		P3:              quarantine.Defaults(),
	}

	if v := getenv("BUILDLENS_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}

	cfg.DatabaseURL = getenv("BUILDLENS_DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("BUILDLENS_DATABASE_URL is required")
	}

	if v := getenv("BUILDLENS_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("BUILDLENS_SHUTDOWN_TIMEOUT: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("BUILDLENS_SHUTDOWN_TIMEOUT must be positive, got %s", d)
		}
		cfg.ShutdownTimeout = d
	}

	if v := getenv("BUILDLENS_LOG_LEVEL"); v != "" {
		// slog.Level understands "debug", "info", "warn" and "error", case-insensitively.
		if err := cfg.LogLevel.UnmarshalText([]byte(strings.TrimSpace(v))); err != nil {
			return Config{}, fmt.Errorf("BUILDLENS_LOG_LEVEL: %w", err)
		}
	}

	if v := getenv("BUILDLENS_MIGRATE_ON_START"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("BUILDLENS_MIGRATE_ON_START: %w", err)
		}
		cfg.MigrateOnStart = b
	}

	cfg.IngestKey = getenv("BUILDLENS_INGEST_KEY")
	if len(cfg.IngestKey) < MinKeyLength {
		// The value itself is never put in the error: errors end up in logs.
		return Config{}, fmt.Errorf("BUILDLENS_INGEST_KEY is required and must be at least %d characters", MinKeyLength)
	}

	if v := getenv("BUILDLENS_MAX_UPLOAD_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("BUILDLENS_MAX_UPLOAD_BYTES: %w", err)
		}
		if n < 1<<20 {
			return Config{}, fmt.Errorf("BUILDLENS_MAX_UPLOAD_BYTES must be at least 1 MiB, got %d", n)
		}
		cfg.MaxUploadBytes = n
	}

	if path := getenv("BUILDLENS_RULES_FILE"); path != "" {
		if err := loadPolicy(path, &cfg.P3); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func loadPolicy(path string, policy *quarantine.Config) error {
	f, err := os.Open(path) //nolint:gosec // trusted server startup configuration
	if err != nil {
		return fmt.Errorf("open BUILDLENS_RULES_FILE: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only config
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return fmt.Errorf("read P3 policy: %w", err)
	}
	if len(data) > 1<<20 {
		return errors.New("P3 policy exceeds 1 MiB")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(policy); err != nil {
		return fmt.Errorf("decode P3 policy: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("P3 policy must contain exactly one YAML document")
	}
	return policy.Validate()
}
