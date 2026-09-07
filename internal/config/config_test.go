package config

import (
	"strings"
	"testing"
)

// validBaseEnv returns a complete valid environment map.
func validBaseEnv() map[string]string {
	return map[string]string{
		"CLICKHOUSE_HOST":      "ch.example.com",
		"CLICKHOUSE_USER":      "default",
		"CLICKHOUSE_PASSWORD":  "secret-pass",
		"GOOGLE_CLOUD_PROJECT": "my-project",
	}
}

// TestLoadMissingRequiredSecrets verifies that missing secrets cause failures.
// It checks that the returned error names the missing variable.
func TestLoadMissingRequiredSecrets(t *testing.T) {
	tests := []struct {
		name        string
		modifyEnv   func(env map[string]string)
		expectedErr string
	}{
		{
			name: "missing CLICKHOUSE_HOST",
			modifyEnv: func(env map[string]string) {
				delete(env, "CLICKHOUSE_HOST")
			},
			expectedErr: "CLICKHOUSE_HOST",
		},
		{
			name: "empty CLICKHOUSE_HOST",
			modifyEnv: func(env map[string]string) {
				env["CLICKHOUSE_HOST"] = "   "
			},
			expectedErr: "CLICKHOUSE_HOST",
		},
		{
			name: "missing CLICKHOUSE_USER",
			modifyEnv: func(env map[string]string) {
				delete(env, "CLICKHOUSE_USER")
			},
			expectedErr: "CLICKHOUSE_USER",
		},
		{
			name: "empty CLICKHOUSE_USER",
			modifyEnv: func(env map[string]string) {
				env["CLICKHOUSE_USER"] = ""
			},
			expectedErr: "CLICKHOUSE_USER",
		},
		{
			name: "missing CLICKHOUSE_PASSWORD",
			modifyEnv: func(env map[string]string) {
				delete(env, "CLICKHOUSE_PASSWORD")
			},
			expectedErr: "CLICKHOUSE_PASSWORD",
		},
		{
			name: "empty CLICKHOUSE_PASSWORD",
			modifyEnv: func(env map[string]string) {
				env["CLICKHOUSE_PASSWORD"] = "   "
			},
			expectedErr: "CLICKHOUSE_PASSWORD",
		},
		{
			name: "missing Google credentials",
			modifyEnv: func(env map[string]string) {
				delete(env, "GOOGLE_CLOUD_PROJECT")
				delete(env, "GOOGLE_APPLICATION_CREDENTIALS")
				delete(env, "GEMINI_API_KEY")
			},
			expectedErr: "GOOGLE_CLOUD_PROJECT",
		},
		{
			name: "empty Google credentials",
			modifyEnv: func(env map[string]string) {
				env["GOOGLE_CLOUD_PROJECT"] = ""
				env["GOOGLE_APPLICATION_CREDENTIALS"] = " "
				env["GEMINI_API_KEY"] = ""
			},
			expectedErr: "GOOGLE_CLOUD_PROJECT",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := validBaseEnv()
			tc.modifyEnv(env)

			cfg, err := LoadFromMap(env)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.expectedErr)
			}
			if cfg != nil {
				t.Fatalf("expected nil config on failure, got %+v", cfg)
			}
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Fatalf("expected error containing %q, got %q", tc.expectedErr, err.Error())
			}
		})
	}
}

// TestGoogleCredentialAlternatives verifies that providing any one Google credential succeeds.
func TestGoogleCredentialAlternatives(t *testing.T) {
	t.Run("GOOGLE_APPLICATION_CREDENTIALS alone satisfies requirement", func(t *testing.T) {
		env := map[string]string{
			"CLICKHOUSE_HOST":                "ch.example.com",
			"CLICKHOUSE_USER":                "default",
			"CLICKHOUSE_PASSWORD":            "secret-pass",
			"GOOGLE_APPLICATION_CREDENTIALS": "/path/to/key.json",
		}

		cfg, err := LoadFromMap(env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.GoogleApplicationCredentials != "/path/to/key.json" {
			t.Fatalf("expected /path/to/key.json, got %q", cfg.GoogleApplicationCredentials)
		}
	})

	t.Run("GEMINI_API_KEY alone satisfies requirement", func(t *testing.T) {
		env := map[string]string{
			"CLICKHOUSE_HOST":     "ch.example.com",
			"CLICKHOUSE_USER":     "default",
			"CLICKHOUSE_PASSWORD": "secret-pass",
			"GEMINI_API_KEY":      "test-api-key",
		}

		cfg, err := LoadFromMap(env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.GeminiAPIKey != "test-api-key" {
			t.Fatalf("expected test-api-key, got %q", cfg.GeminiAPIKey)
		}
	})
}

// TestLoadDefaults verifies default values for omitted optional variables.
func TestLoadDefaults(t *testing.T) {
	env := validBaseEnv()

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("expected default Port '8080', got %q", cfg.Port)
	}
	if cfg.Env != "development" {
		t.Errorf("expected default Env 'development', got %q", cfg.Env)
	}
	if cfg.GeminiModel != "gemini-3.8-flash" {
		t.Errorf("expected default GeminiModel 'gemini-3.8-flash', got %q", cfg.GeminiModel)
	}
	if cfg.ClickHousePort != 8443 {
		t.Errorf("expected default ClickHousePort 8443, got %d", cfg.ClickHousePort)
	}
	if cfg.ClickHouseDatabase != "default" {
		t.Errorf("expected default ClickHouseDatabase 'default', got %q", cfg.ClickHouseDatabase)
	}
	if cfg.ClickHouseSecure != true {
		t.Errorf("expected default ClickHouseSecure true, got %v", cfg.ClickHouseSecure)
	}
	if cfg.GoogleCloudLocation != "us-central1" {
		t.Errorf("expected default GoogleCloudLocation 'us-central1', got %q", cfg.GoogleCloudLocation)
	}
}

// TestLoadValidCustomConfiguration verifies loading all custom variables.
func TestLoadValidCustomConfiguration(t *testing.T) {
	env := map[string]string{
		"PORT":                           "9090",
		"ENV":                            "production",
		"GEMINI_MODEL":                   "gemini-2.0-flash",
		"GEMINI_API_KEY":                 "my-gemini-key",
		"GOOGLE_CLOUD_PROJECT":           "prod-project",
		"GOOGLE_CLOUD_LOCATION":          "europe-west1",
		"GOOGLE_APPLICATION_CREDENTIALS": "/var/run/secrets/google.json",
		"VERTEX_OPENAPI_BASE_URL":        "https://vertex.custom.endpoint",
		"CLICKHOUSE_HOST":                "prod-ch.cloud",
		"CLICKHOUSE_PORT":                "9440",
		"CLICKHOUSE_USER":                "prod-admin",
		"CLICKHOUSE_PASSWORD":            "super-secret",
		"CLICKHOUSE_DATABASE":            "events",
		"CLICKHOUSE_SECURE":              "false",
		"CLICKHOUSE_KEY_ID":              "key-abc",
		"CLICKHOUSE_KEY_SECRET":          "secret-def",
		"CLICKHOUSE_SERVICE_ID":          "svc-123",
		"CLICKHOUSE_ORG_ID":              "org-456",
	}

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("expected Port '9090', got %q", cfg.Port)
	}
	if cfg.Env != "production" {
		t.Errorf("expected Env 'production', got %q", cfg.Env)
	}
	if cfg.GeminiModel != "gemini-2.0-flash" {
		t.Errorf("expected GeminiModel 'gemini-2.0-flash', got %q", cfg.GeminiModel)
	}
	if cfg.GeminiAPIKey != "my-gemini-key" {
		t.Errorf("expected GeminiAPIKey 'my-gemini-key', got %q", cfg.GeminiAPIKey)
	}
	if cfg.GoogleCloudProject != "prod-project" {
		t.Errorf("expected GoogleCloudProject 'prod-project', got %q", cfg.GoogleCloudProject)
	}
	if cfg.GoogleCloudLocation != "europe-west1" {
		t.Errorf("expected GoogleCloudLocation 'europe-west1', got %q", cfg.GoogleCloudLocation)
	}
	if cfg.GoogleApplicationCredentials != "/var/run/secrets/google.json" {
		t.Errorf("expected GoogleApplicationCredentials '/var/run/secrets/google.json', got %q", cfg.GoogleApplicationCredentials)
	}
	if cfg.VertexOpenAPIBaseURL != "https://vertex.custom.endpoint" {
		t.Errorf("expected VertexOpenAPIBaseURL 'https://vertex.custom.endpoint', got %q", cfg.VertexOpenAPIBaseURL)
	}
	if cfg.ClickHouseHost != "prod-ch.cloud" {
		t.Errorf("expected ClickHouseHost 'prod-ch.cloud', got %q", cfg.ClickHouseHost)
	}
	if cfg.ClickHousePort != 9440 {
		t.Errorf("expected ClickHousePort 9440, got %d", cfg.ClickHousePort)
	}
	if cfg.ClickHouseUser != "prod-admin" {
		t.Errorf("expected ClickHouseUser 'prod-admin', got %q", cfg.ClickHouseUser)
	}
	if cfg.ClickHousePassword != "super-secret" {
		t.Errorf("expected ClickHousePassword 'super-secret', got %q", cfg.ClickHousePassword)
	}
	if cfg.ClickHouseDatabase != "events" {
		t.Errorf("expected ClickHouseDatabase 'events', got %q", cfg.ClickHouseDatabase)
	}
	if cfg.ClickHouseSecure != false {
		t.Errorf("expected ClickHouseSecure false, got %v", cfg.ClickHouseSecure)
	}
	if cfg.ClickHouseKeyID != "key-abc" {
		t.Errorf("expected ClickHouseKeyID 'key-abc', got %q", cfg.ClickHouseKeyID)
	}
	if cfg.ClickHouseKeySecret != "secret-def" {
		t.Errorf("expected ClickHouseKeySecret 'secret-def', got %q", cfg.ClickHouseKeySecret)
	}
	if cfg.ClickHouseServiceID != "svc-123" {
		t.Errorf("expected ClickHouseServiceID 'svc-123', got %q", cfg.ClickHouseServiceID)
	}
	if cfg.ClickHouseOrgID != "org-456" {
		t.Errorf("expected ClickHouseOrgID 'org-456', got %q", cfg.ClickHouseOrgID)
	}
}

// TestLoadInvalidPort verifies that non-numeric port strings cause errors.
func TestLoadInvalidPort(t *testing.T) {
	env := validBaseEnv()
	env["CLICKHOUSE_PORT"] = "not-a-number"

	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatal("expected error for invalid CLICKHOUSE_PORT, got nil")
	}
	if !strings.Contains(err.Error(), "CLICKHOUSE_PORT") {
		t.Fatalf("expected error mentioning CLICKHOUSE_PORT, got %q", err.Error())
	}
}

// TestLoadInvalidSecure verifies that invalid boolean strings cause errors.
func TestLoadInvalidSecure(t *testing.T) {
	env := validBaseEnv()
	env["CLICKHOUSE_SECURE"] = "not-a-boolean"

	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatal("expected error for invalid CLICKHOUSE_SECURE, got nil")
	}
	if !strings.Contains(err.Error(), "CLICKHOUSE_SECURE") {
		t.Fatalf("expected error mentioning CLICKHOUSE_SECURE, got %q", err.Error())
	}
}

// TestLoadWithOsEnv verifies Load reads directly from process environment variables.
func TestLoadWithOsEnv(t *testing.T) {
	t.Setenv("CLICKHOUSE_HOST", "env-ch.example.com")
	t.Setenv("CLICKHOUSE_USER", "env-user")
	t.Setenv("CLICKHOUSE_PASSWORD", "env-pass")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")
	t.Setenv("PORT", "3000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error from Load(): %v", err)
	}

	if cfg.ClickHouseHost != "env-ch.example.com" {
		t.Errorf("expected ClickHouseHost 'env-ch.example.com', got %q", cfg.ClickHouseHost)
	}
	if cfg.Port != "3000" {
		t.Errorf("expected Port '3000', got %q", cfg.Port)
	}
}
