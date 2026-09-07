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
	Port string
	Env  string

	// Gemini and Google Cloud settings.
	GeminiModel                  string
	GeminiAPIKey                 string
	GoogleCloudProject           string
	GoogleCloudLocation          string
	GoogleApplicationCredentials string
	VertexOpenAPIBaseURL         string

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
	DefaultGeminiModel        = "gemini-3.8-flash"
	DefaultClickHousePort     = 8443
	DefaultClickHouseDatabase = "default"
	DefaultClickHouseSecure   = true
	DefaultGoogleLocation     = "us-central1"
)

// Load reads configuration from process environment variables.
// It fails immediately when any required secret is missing or empty.
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

	chHost := get("CLICKHOUSE_HOST")
	if chHost == "" {
		return nil, errors.New("missing required environment variable: CLICKHOUSE_HOST")
	}

	chUser := get("CLICKHOUSE_USER")
	if chUser == "" {
		return nil, errors.New("missing required environment variable: CLICKHOUSE_USER")
	}

	chPassword := get("CLICKHOUSE_PASSWORD")
	if chPassword == "" {
		return nil, errors.New("missing required environment variable: CLICKHOUSE_PASSWORD")
	}

	project := get("GOOGLE_CLOUD_PROJECT")
	appCreds := get("GOOGLE_APPLICATION_CREDENTIALS")
	geminiKey := get("GEMINI_API_KEY")
	if project == "" && appCreds == "" && geminiKey == "" {
		return nil, errors.New("missing required environment variable: GOOGLE_CLOUD_PROJECT (or GOOGLE_APPLICATION_CREDENTIALS or GEMINI_API_KEY)")
	}

	port := get("PORT")
	if port == "" {
		port = DefaultPort
	}

	env := get("ENV")
	if env == "" {
		env = DefaultEnv
	}

	geminiModel := get("GEMINI_MODEL")
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

	return &Config{
		Port:                         port,
		Env:                          env,
		GeminiModel:                  geminiModel,
		GeminiAPIKey:                 geminiKey,
		GoogleCloudProject:           project,
		GoogleCloudLocation:          location,
		GoogleApplicationCredentials: appCreds,
		VertexOpenAPIBaseURL:         get("VERTEX_OPENAPI_BASE_URL"),
		ClickHouseHost:               chHost,
		ClickHousePort:               chPort,
		ClickHouseUser:               chUser,
		ClickHousePassword:           chPassword,
		ClickHouseDatabase:           chDatabase,
		ClickHouseSecure:             chSecure,
		ClickHouseKeyID:              get("CLICKHOUSE_KEY_ID"),
		ClickHouseKeySecret:          get("CLICKHOUSE_KEY_SECRET"),
		ClickHouseServiceID:          get("CLICKHOUSE_SERVICE_ID"),
		ClickHouseOrgID:              get("CLICKHOUSE_ORG_ID"),
	}, nil
}
