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
	"github.com/nibinrj/buildlens/internal/regression"
)

// Config is the server configuration. Every field comes from an environment variable.
type Config struct {
	HTTPAddr           string            // BUILDLENS_HTTP_ADDR, default ":8080"
	DatabaseURL        string            // BUILDLENS_DATABASE_URL, required
	ShutdownTimeout    time.Duration     // BUILDLENS_SHUTDOWN_TIMEOUT, default 15s
	LogLevel           slog.Level        // BUILDLENS_LOG_LEVEL: debug, info, warn, error; default info
	MigrateOnStart     bool              // BUILDLENS_MIGRATE_ON_START, default true
	IngestKey          string            // BUILDLENS_INGEST_KEY, required, at least MinKeyLength characters; never logged
	MaxUploadBytes     int64             // BUILDLENS_MAX_UPLOAD_BYTES, default 64 MiB: the largest POST /api/v1/builds body
	P3                 quarantine.Config // BUILDLENS_RULES_FILE optional strict YAML; otherwise defaults
	Regression         regression.Config
	ReferenceInstance  string
	PriceRegion        string
	GitHubToken        string // BUILDLENS_GITHUB_TOKEN; separate from the read-only Jenkins token
	GitHubRepositories string // comma separated allowlist; required when the token is set
}

// MinKeyLength is the shortest API key the server accepts, so a placeholder like "changeme" fails at start.
const MinKeyLength = 16

// Load builds a Config from getenv. Pass os.Getenv in production and a map lookup in tests.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:          ":8080",
		ShutdownTimeout:   15 * time.Second,
		LogLevel:          slog.LevelInfo,
		MigrateOnStart:    true,
		MaxUploadBytes:    64 << 20,
		P3:                quarantine.Defaults(),
		Regression:        regression.Defaults(),
		ReferenceInstance: "t3.small",
		PriceRegion:       "us-east-1",
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
		if err := loadPolicy(path, &cfg.P3, &cfg.Regression); err != nil {
			return Config{}, err
		}
	}
	if v := getenv("BUILDLENS_REFERENCE_INSTANCE"); v != "" {
		cfg.ReferenceInstance = v
	}
	if v := getenv("BUILDLENS_PRICE_REGION"); v != "" {
		cfg.PriceRegion = v
	}
	cfg.GitHubToken = getenv("BUILDLENS_GITHUB_TOKEN")
	cfg.GitHubRepositories = strings.TrimSpace(getenv("BUILDLENS_GITHUB_REPOSITORIES"))
	if cfg.GitHubToken != "" && cfg.GitHubRepositories == "" {
		return Config{}, errors.New("BUILDLENS_GITHUB_REPOSITORIES is required when GitHub comments are enabled")
	}
	for _, repo := range strings.Split(cfg.GitHubRepositories, ",") {
		if cfg.GitHubRepositories == "" {
			break
		}
		parts := strings.Split(strings.TrimSpace(repo), "/")
		if len(parts) != 2 || !validRepoPart(parts[0]) || !validRepoPart(parts[1]) {
			return Config{}, errors.New("BUILDLENS_GITHUB_REPOSITORIES must contain owner/name entries")
		}
	}
	return cfg, nil
}

func validRepoPart(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func loadPolicy(path string, policy *quarantine.Config, regressions ...*regression.Config) error {
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
	doc := struct {
		quarantine.Config `yaml:",inline"`
		Regression        regression.Config `yaml:"regression"`
	}{*policy, regression.Defaults()}
	if len(regressions) > 0 {
		doc.Regression = *regressions[0]
	}
	if err := d.Decode(&doc); err != nil {
		return fmt.Errorf("decode P3 policy: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("P3 policy must contain exactly one YAML document")
	}
	if err := doc.Regression.Validate(); err != nil {
		return err
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	*policy = doc.Config
	if len(regressions) > 0 {
		*regressions[0] = doc.Regression
	}
	return nil
}
