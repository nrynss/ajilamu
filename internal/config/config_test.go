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

// TestDeferredCredentials verifies fixture mode loads process settings without
// credentials, then names the missing setting at the feature boundary.
func TestDeferredCredentials(t *testing.T) {
	tests := []struct {
		name        string
		modifyEnv   func(env map[string]string)
		expectedErr string
		require     func(*Config) error
	}{
		{
			name: "missing CLICKHOUSE_HOST",
			modifyEnv: func(env map[string]string) {
				delete(env, "CLICKHOUSE_HOST")
			},
			expectedErr: "CLICKHOUSE_HOST",
			require:     (*Config).RequireClickHouse,
		},
		{
			name: "empty CLICKHOUSE_HOST",
			modifyEnv: func(env map[string]string) {
				env["CLICKHOUSE_HOST"] = "   "
			},
			expectedErr: "CLICKHOUSE_HOST",
			require:     (*Config).RequireClickHouse,
		},
		{
			name: "missing CLICKHOUSE_USER",
			modifyEnv: func(env map[string]string) {
				delete(env, "CLICKHOUSE_USER")
			},
			expectedErr: "CLICKHOUSE_USER",
			require:     (*Config).RequireClickHouse,
		},
		{
			name: "empty CLICKHOUSE_USER",
			modifyEnv: func(env map[string]string) {
				env["CLICKHOUSE_USER"] = ""
			},
			expectedErr: "CLICKHOUSE_USER",
			require:     (*Config).RequireClickHouse,
		},
		{
			name: "missing CLICKHOUSE_PASSWORD",
			modifyEnv: func(env map[string]string) {
				delete(env, "CLICKHOUSE_PASSWORD")
			},
			expectedErr: "CLICKHOUSE_PASSWORD",
			require:     (*Config).RequireClickHouse,
		},
		{
			name: "empty CLICKHOUSE_PASSWORD",
			modifyEnv: func(env map[string]string) {
				env["CLICKHOUSE_PASSWORD"] = "   "
			},
			expectedErr: "CLICKHOUSE_PASSWORD",
			require:     (*Config).RequireClickHouse,
		},
		{
			name: "missing GOOGLE_CLOUD_PROJECT",
			modifyEnv: func(env map[string]string) {
				delete(env, "GOOGLE_CLOUD_PROJECT")
			},
			expectedErr: "GOOGLE_CLOUD_PROJECT",
			require:     (*Config).RequireGoogleCloud,
		},
		{
			name: "empty GOOGLE_CLOUD_PROJECT",
			modifyEnv: func(env map[string]string) {
				env["GOOGLE_CLOUD_PROJECT"] = " "
			},
			expectedErr: "GOOGLE_CLOUD_PROJECT",
			require:     (*Config).RequireGoogleCloud,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := validBaseEnv()
			tc.modifyEnv(env)

			cfg, err := LoadFromMap(env)
			if err != nil {
				t.Fatalf("fixture settings should load: %v", err)
			}
			err = tc.require(cfg)
			if err == nil {
				t.Fatalf("expected feature error containing %q, got nil", tc.expectedErr)
			}
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Fatalf("expected error containing %q, got %q", tc.expectedErr, err.Error())
			}
		})
	}
}

// TestGoogleProjectRequirement verifies credential-file settings do not replace
// the project identifier required by Google Cloud clients.
func TestGoogleProjectRequirement(t *testing.T) {
	t.Run("GOOGLE_APPLICATION_CREDENTIALS does not replace project", func(t *testing.T) {
		env := map[string]string{
			"CLICKHOUSE_HOST":                "ch.example.com",
			"CLICKHOUSE_USER":                "default",
			"CLICKHOUSE_PASSWORD":            "secret-pass",
			"GOOGLE_APPLICATION_CREDENTIALS": "/path/to/key.json",
		}

		cfg, err := LoadFromMap(env)
		if err != nil {
			t.Fatalf("fixture settings should load: %v", err)
		}
		err = cfg.RequireGoogleCloud()
		if err == nil {
			t.Fatal("expected error naming GOOGLE_CLOUD_PROJECT, got nil")
		}
		if !strings.Contains(err.Error(), "GOOGLE_CLOUD_PROJECT") {
			t.Fatalf("expected error containing GOOGLE_CLOUD_PROJECT, got %q", err.Error())
		}
	})

	t.Run("GEMINI_API_KEY does not replace project", func(t *testing.T) {
		env := map[string]string{
			"CLICKHOUSE_HOST":     "ch.example.com",
			"CLICKHOUSE_USER":     "default",
			"CLICKHOUSE_PASSWORD": "secret-pass",
			"GEMINI_API_KEY":      "test-api-key",
		}

		cfg, err := LoadFromMap(env)
		if err != nil {
			t.Fatalf("fixture settings should load: %v", err)
		}
		err = cfg.RequireGoogleCloud()
		if err == nil {
			t.Fatal("expected error naming GOOGLE_CLOUD_PROJECT, got nil")
		}
		if !strings.Contains(err.Error(), "GOOGLE_CLOUD_PROJECT") {
			t.Fatalf("expected error containing GOOGLE_CLOUD_PROJECT, got %q", err.Error())
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
	if cfg.DataDir != DefaultDataDir {
		t.Errorf("expected default DataDir %q, got %q", DefaultDataDir, cfg.DataDir)
	}
	if cfg.FrontendDir != "" {
		t.Errorf("expected empty default FrontendDir, got %q", cfg.FrontendDir)
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
	if cfg.GoogleCloudLocation != "global" {
		t.Errorf("expected default GoogleCloudLocation 'global', got %q", cfg.GoogleCloudLocation)
	}
	if cfg.GoogleApplicationCredentials != "" {
		t.Errorf("expected empty GoogleApplicationCredentials, got %q", cfg.GoogleApplicationCredentials)
	}
}

// TestFixtureModeStartsWithoutCredentials pins the no-environment startup
// contract used by the offline workspace.
func TestFixtureModeStartsWithoutCredentials(t *testing.T) {
	cfg, err := LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("LoadFromMap without credentials: %v", err)
	}
	if cfg.Port != DefaultPort || cfg.Env != DefaultEnv {
		t.Fatalf("process defaults = port %q env %q", cfg.Port, cfg.Env)
	}
}

// TestProductionRequiresCredentials keeps deployed hosts from silently
// falling back to fixture mode.
func TestProductionRequiresCredentials(t *testing.T) {
	tests := []struct {
		name  string
		unset string
		want  string
	}{
		{"ClickHouse host", "CLICKHOUSE_HOST", "CLICKHOUSE_HOST"},
		{"ClickHouse user", "CLICKHOUSE_USER", "CLICKHOUSE_USER"},
		{"ClickHouse password", "CLICKHOUSE_PASSWORD", "CLICKHOUSE_PASSWORD"},
		{"Google Cloud project", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_PROJECT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := validBaseEnv()
			env["ENV"] = "production"
			delete(env, tc.unset)
			cfg, err := LoadFromMap(env)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadFromMap error = %v, want name %q", err, tc.want)
			}
			if cfg != nil {
				t.Fatalf("production config = %+v, want nil", cfg)
			}
		})
	}
}

// TestLoadStripsGoogleModelPrefix verifies the Model Garden prefix cannot reach Vertex.
func TestLoadStripsGoogleModelPrefix(t *testing.T) {
	env := validBaseEnv()
	env["GEMINI_MODEL"] = "google/gemini-3.8-flash"

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GeminiModel != "gemini-3.8-flash" {
		t.Errorf("GeminiModel = %q, want gemini-3.8-flash", cfg.GeminiModel)
	}
}

// TestLoadValidCustomConfiguration verifies loading all custom variables.
func TestLoadValidCustomConfiguration(t *testing.T) {
	env := map[string]string{
		"PORT":                           "9090",
		"ENV":                            "production",
		"AJILAMU_DATA_DIR":               "/var/lib/ajilamu",
		"AJILAMU_FRONTEND_DIR":           "/srv/ajilamu/web",
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
	if cfg.DataDir != "/var/lib/ajilamu" {
		t.Errorf("expected DataDir '/var/lib/ajilamu', got %q", cfg.DataDir)
	}
	if cfg.FrontendDir != "/srv/ajilamu/web" {
		t.Errorf("expected FrontendDir '/srv/ajilamu/web', got %q", cfg.FrontendDir)
	}
	if cfg.GeminiModel != "gemini-2.0-flash" {
		t.Errorf("expected GeminiModel 'gemini-2.0-flash', got %q", cfg.GeminiModel)
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
	t.Setenv("ENV", DefaultEnv)
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

// TestMCPOptionalSettingsAreRead reads all five MCP variables as settings.
func TestMCPOptionalSettingsAreRead(t *testing.T) {
	env := validBaseEnv()
	env["CLICKHOUSE_MCP_URL"] = "http://127.0.0.1:8000/mcp"
	env["CLICKHOUSE_MCP_SERVER_TRANSPORT"] = "http"
	env["CLICKHOUSE_MCP_ALLOWED_HOSTS"] = "127.0.0.1:8000,localhost:8000"
	env["CLICKHOUSE_MCP_AUTH_TOKEN"] = "token-value"
	env["CLICKHOUSE_READONLY_PASSWORD"] = "readonly-pass"

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClickHouseMCPURL != "http://127.0.0.1:8000/mcp" {
		t.Errorf("ClickHouseMCPURL = %q", cfg.ClickHouseMCPURL)
	}
	if cfg.ClickHouseMCPServerTransport != "http" {
		t.Errorf("ClickHouseMCPServerTransport = %q", cfg.ClickHouseMCPServerTransport)
	}
	if cfg.ClickHouseMCPAllowedHosts != "127.0.0.1:8000,localhost:8000" {
		t.Errorf("ClickHouseMCPAllowedHosts = %q", cfg.ClickHouseMCPAllowedHosts)
	}
	if cfg.ClickHouseMCPAuthToken != "token-value" {
		t.Errorf("ClickHouseMCPAuthToken = %q", cfg.ClickHouseMCPAuthToken)
	}
	if cfg.ClickHouseReadonlyPassword != "readonly-pass" {
		t.Errorf("ClickHouseReadonlyPassword = %q", cfg.ClickHouseReadonlyPassword)
	}
	if !cfg.MCPConfigured() {
		t.Error("MCPConfigured() = false, want true with a URL and token")
	}
}

// TestMissingMCPDisablesAgentNotServer pins the optional contract.
// A missing MCP setting disables the editor agent and never stops the server.
func TestMissingMCPDisablesAgentNotServer(t *testing.T) {
	t.Run("fixture mode", func(t *testing.T) {
		cfg, err := LoadFromMap(map[string]string{})
		if err != nil {
			t.Fatalf("LoadFromMap without MCP settings: %v", err)
		}
		if cfg.MCPConfigured() {
			t.Error("MCPConfigured() = true without a URL and token")
		}
	})

	t.Run("production without MCP", func(t *testing.T) {
		env := validBaseEnv()
		env["ENV"] = "production"
		cfg, err := LoadFromMap(env)
		if err != nil {
			t.Fatalf("production without MCP settings: %v", err)
		}
		if cfg.MCPConfigured() {
			t.Error("MCPConfigured() = true without a URL and token")
		}
	})

	t.Run("token alone does not configure", func(t *testing.T) {
		env := validBaseEnv()
		env["CLICKHOUSE_MCP_AUTH_TOKEN"] = "token-value"
		cfg, err := LoadFromMap(env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.MCPConfigured() {
			t.Error("MCPConfigured() = true without a URL")
		}
	})
}
