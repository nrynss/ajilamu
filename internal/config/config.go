package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds runtime configuration for the application.
type Config struct {
	// Server settings.
	Port        string
	Env         string
	DataDir     string
	FrontendDir string

	// Gemini and Google Cloud settings.
	GeminiModel                  string
	GoogleCloudProject           string
	GoogleCloudLocation          string
	GoogleApplicationCredentials string

	// ClickHouse Cloud database settings.
	ClickHouseHost     string
	ClickHousePort     int
	ClickHouseUser     string
	ClickHousePassword string
	ClickHouseDatabase string
	ClickHouseSecure   bool

	// ClickHouse Cloud OpenAPI settings.
	ClickHouseKeyID     string
	ClickHouseKeySecret string
	ClickHouseServiceID string
	ClickHouseOrgID     string
}

// LookupEnvFunc reads an environment variable by name.
type LookupEnvFunc func(key string) (string, bool)

// Default configuration values.
const (
	DefaultPort               = "8080"
	DefaultEnv                = "development"
	DefaultDataDir            = "data"
	DefaultGeminiModel        = "gemini-3.8-flash"
	DefaultClickHousePort     = 8443
	DefaultClickHouseDatabase = "default"
	DefaultClickHouseSecure   = true
	DefaultGoogleLocation     = "global"
)

// Load reads process settings from process environment variables. Development
// and fixture mode defer credentials until a feature needs them. Production
// validates all boot-time credentials before accepting traffic.
func Load() (*Config, error) {
	return LoadFromLookup(os.LookupEnv)
}

// LoadFromMap reads configuration from an in-memory map.
func LoadFromMap(env map[string]string) (*Config, error) {
	return LoadFromLookup(func(key string) (string, bool) {
		val, ok := env[key]
		return val, ok
	})
}

// LoadFromLookup reads configuration using a lookup function.
func LoadFromLookup(lookup LookupEnvFunc) (*Config, error) {
	get := func(key string) string {
		val, _ := lookup(key)
		return strings.TrimSpace(val)
	}

	port := get("PORT")
	if port == "" {
		port = DefaultPort
	}

	env := get("ENV")
	if env == "" {
		env = DefaultEnv
	}

	dataDir := get("AJILAMU_DATA_DIR")
	if dataDir == "" {
		dataDir = DefaultDataDir
	}

	// An empty frontend directory leaves build discovery to the entrypoint.
	frontendDir := get("AJILAMU_FRONTEND_DIR")

	geminiModel := get("GEMINI_MODEL")
	geminiModel = strings.TrimPrefix(geminiModel, "google/")
	if geminiModel == "" {
		geminiModel = DefaultGeminiModel
	}

	chPort := DefaultClickHousePort
	if portStr := get("CLICKHOUSE_PORT"); portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil || p <= 0 || p > 65535 {
			return nil, fmt.Errorf("invalid integer for CLICKHOUSE_PORT: %s", portStr)
		}
		chPort = p
	}

	chDatabase := get("CLICKHOUSE_DATABASE")
	if chDatabase == "" {
		chDatabase = DefaultClickHouseDatabase
	}

	chSecure := DefaultClickHouseSecure
	if secureStr := get("CLICKHOUSE_SECURE"); secureStr != "" {
		s, err := strconv.ParseBool(secureStr)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean for CLICKHOUSE_SECURE: %s", secureStr)
		}
		chSecure = s
	}

	location := get("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = DefaultGoogleLocation
	}

	cfg := &Config{
		Port:                         port,
		Env:                          env,
		DataDir:                      dataDir,
		FrontendDir:                  frontendDir,
		GeminiModel:                  geminiModel,
		GoogleCloudProject:           get("GOOGLE_CLOUD_PROJECT"),
		GoogleCloudLocation:          location,
		GoogleApplicationCredentials: get("GOOGLE_APPLICATION_CREDENTIALS"),
		ClickHouseHost:               get("CLICKHOUSE_HOST"),
		ClickHousePort:               chPort,
		ClickHouseUser:               get("CLICKHOUSE_USER"),
		ClickHousePassword:           get("CLICKHOUSE_PASSWORD"),
		ClickHouseDatabase:           chDatabase,
		ClickHouseSecure:             chSecure,
		ClickHouseKeyID:              get("CLICKHOUSE_KEY_ID"),
		ClickHouseKeySecret:          get("CLICKHOUSE_KEY_SECRET"),
		ClickHouseServiceID:          get("CLICKHOUSE_SERVICE_ID"),
		ClickHouseOrgID:              get("CLICKHOUSE_ORG_ID"),
	}
	if cfg.Env == "production" {
		if err := cfg.RequireProductionCredentials(); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// RequireClickHouse returns an error naming the first missing variable needed
// to write the durable ledger. It does not affect fixture-only requests.
func (c *Config) RequireClickHouse() error {
	if c == nil {
		return errors.New("config is nil")
	}
	for _, requirement := range []struct {
		name  string
		value string
	}{
		{"CLICKHOUSE_HOST", c.ClickHouseHost},
		{"CLICKHOUSE_USER", c.ClickHouseUser},
		{"CLICKHOUSE_PASSWORD", c.ClickHousePassword},
	} {
		if strings.TrimSpace(requirement.value) == "" {
			return fmt.Errorf("missing required environment variable: %s", requirement.name)
		}
	}
	return nil
}

// RequireGoogleCloud returns an error naming the project required for Gemini
// and Chirp calls. ADC itself remains the SDK's responsibility.
func (c *Config) RequireGoogleCloud() error {
	if c == nil {
		return errors.New("config is nil")
	}
	if strings.TrimSpace(c.GoogleCloudProject) == "" {
		return errors.New("missing required environment variable: GOOGLE_CLOUD_PROJECT")
	}
	return nil
}

// RequireProductionCredentials validates every credential-bearing feature at
// boot. Production never starts in degraded fixture mode.
func (c *Config) RequireProductionCredentials() error {
	if err := c.RequireClickHouse(); err != nil {
		return err
	}
	return c.RequireGoogleCloud()
}
