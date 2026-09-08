package ledger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/config"
)

func TestDurationPriorCapture(t *testing.T) {
	type reply struct {
		status int
		body   string
	}
	replies := map[string]reply{
		"cold":      {body: `{"population_samples":30,"population_chars_per_sec":2,"creator_samples":0,"creator_chars_per_sec":0}`},
		"ten":       {body: `{"population_samples":50,"population_chars_per_sec":2,"creator_samples":10,"creator_chars_per_sec":4}`},
		"hundred":   {body: `{"population_samples":140,"population_chars_per_sec":2,"creator_samples":100,"creator_chars_per_sec":4}`},
		"malformed": {body: `{not JSON`},
		"empty":     {body: ""},
		"zero":      {body: `{"population_samples":0,"population_chars_per_sec":0,"creator_samples":0,"creator_chars_per_sec":0}`},
		"failure":   {status: http.StatusInternalServerError, body: "database unavailable"},
	}
	var requests []*http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Clone(r.Context()))
		response, ok := replies[r.URL.Query().Get("param_owner_id")]
		if !ok {
			http.Error(w, "unexpected owner", http.StatusBadRequest)
			return
		}
		if response.status != 0 {
			w.WriteHeader(response.status)
		}
		if _, err := w.Write([]byte(response.body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	client, err := New(&config.Config{
		ClickHouseHost: "fixture.invalid", ClickHousePort: 8443, ClickHouseUser: "fixture",
		ClickHousePassword: "fixture", ClickHouseDatabase: "fixture",
	}, t.TempDir(), WithEndpoint(server.URL))
	if err != nil {
		t.Fatalf("new ledger client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	cold, err := client.DurationPrior(ctx, "cold", "ml-IN", "ml-IN-Chirp3-HD")
	if err != nil {
		t.Fatalf("cold DurationPrior: %v", err)
	}
	if cold.PopulationSamples != 30 || cold.CreatorSamples != 0 || cold.CharsPerSecond != 2 || cold.CreatorWeight != 0 {
		t.Errorf("cold prior = %+v, want population fallback with both counts", cold)
	}
	ten, err := client.DurationPrior(ctx, "ten", "ml-IN", "ml-IN-Chirp3-HD")
	if err != nil {
		t.Fatalf("ten DurationPrior: %v", err)
	}
	if ten.PopulationSamples != 50 || ten.CreatorSamples != 10 || ten.CreatorWeight != 0.5 || ten.CharsPerSecond != 3 {
		t.Errorf("ten-sample prior = %+v, want 50 population, 10 creator, weight 0.5, rate 3", ten)
	}
	hundred, err := client.DurationPrior(ctx, "hundred", "ml-IN", "ml-IN-Chirp3-HD")
	if err != nil {
		t.Fatalf("hundred DurationPrior: %v", err)
	}
	if hundred.PopulationSamples != 140 || hundred.CreatorSamples != 100 {
		t.Errorf("hundred-sample counts = %+v, want 140 population and 100 creator", hundred)
	}
	if hundred.CreatorWeight <= ten.CreatorWeight || hundred.CharsPerSecond <= ten.CharsPerSecond || hundred.CharsPerSecond >= 4 {
		t.Errorf("creator influence did not grow toward the creator rate: ten=%+v hundred=%+v", ten, hundred)
	}

	for _, owner := range []string{"malformed", "empty", "zero", "failure"} {
		if _, err := client.DurationPrior(ctx, owner, "ml-IN", "ml-IN-Chirp3-HD"); err == nil {
			t.Errorf("DurationPrior(%q) succeeded, want response error", owner)
		}
	}

	if len(requests) != 7 {
		t.Fatalf("captured %d requests, want 7", len(requests))
	}
	for index, request := range requests {
		if request.Method != http.MethodPost {
			t.Errorf("request %d method = %s, want POST", index, request.Method)
		}
		query := request.URL.Query()
		if got := query.Get("database"); got != "fixture" {
			t.Errorf("request %d database = %q, want fixture", index, got)
		}
		if got := query.Get("param_language"); got != "ml-IN" {
			t.Errorf("request %d language parameter = %q, want ml-IN", index, got)
		}
		if got := query.Get("param_speaker"); got != "ml-IN-Chirp3-HD" {
			t.Errorf("request %d speaker parameter = %q, want ml-IN-Chirp3-HD", index, got)
		}
		statement := query.Get("query")
		for _, required := range []string{"FROM take_rates", "{owner_id:String}", "{language:String}", "{speaker:String}", "population_samples", "creator_samples"} {
			if !strings.Contains(statement, required) {
				t.Errorf("request %d query omitted %q: %s", index, required, statement)
			}
		}
		owner := query.Get("param_owner_id")
		if owner == "" {
			t.Errorf("request %d omitted bound owner parameter", index)
		}
		if strings.Contains(statement, owner) {
			t.Errorf("request %d interpolated owner %q into query", index, owner)
		}
	}
}
