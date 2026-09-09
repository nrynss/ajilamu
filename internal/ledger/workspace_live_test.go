//go:build live

package ledger

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// liveClickHouseOptIn is the explicit switch that lets this probe write to a
// real ClickHouse. Without it the probe skips, so a default .env can never
// point the live test at a production database.
const liveClickHouseOptIn = "AJILAMU_LIVE_CLICKHOUSE"

// liveDub is the only dub this probe writes.
const liveDub = "d-t66c-live"

// liveSeedTemplate inserts one dub with two identical agent turns plus
// translate, synthesize and segment rows. The agent turns bill 768000
// nanodollars and the other rows bill 957100, so a fold that leaks past the
// agent kind reports the wrong units and the wrong total.
const liveSeedTemplate = `INSERT INTO charges_raw (take_id, commit_id, project_id, dub_id, owner_id, language, segment_index, attempt, turn_id, call_index, kind, provider, unit, units, unit_price_usd) VALUES
('', 'c1', 'p1', '{dub}', 'o1', 'ml', 1, 1, '', 0, 'translate', 'gemini-3.8-flash', 'prompt_tokens', 21, 0.0000001),
('', 'c1', 'p1', '{dub}', 'o1', 'ml', 1, 1, '', 0, 'translate', 'gemini-3.8-flash', 'candidate_tokens', 30, 0.0000006),
('', 'c1', 'p1', '{dub}', 'o1', 'ml', 1, 1, '', 0, 'synthesize', 'chirp-3-hd', 'characters', 29, 0.00003),
('', 'c1', 'p1', '{dub}', 'o1', 'ml', 1, 1, '', 0, 'segment', 'gemini-3.8-flash', 'prompt_tokens', 670, 0.0000001),
('', '', 'p1', '{dub}', 'o1', 'ml', -1, 0, 'turn-1', 0, 'agent', 'gemini-3.8-flash', 'prompt_tokens', 1200, 0.00000015),
('', '', 'p1', '{dub}', 'o1', 'ml', -1, 0, 'turn-1', 1, 'agent', 'gemini-3.8-flash', 'candidate_tokens', 340, 0.0000006),
('', '', 'p1', '{dub}', 'o1', 'ml', -1, 0, 'turn-2', 0, 'agent', 'gemini-3.8-flash', 'prompt_tokens', 1200, 0.00000015),
('', '', 'p1', '{dub}', 'o1', 'ml', -1, 0, 'turn-2', 1, 'agent', 'gemini-3.8-flash', 'candidate_tokens', 340, 0.0000006);`

// TestWholePassChargesLiveFoldsAgentRows loads sql/schema.sql into a real
// ClickHouse, seeds one dub with agent, translate, synthesize and segment
// rows, then reads the whole-pass charges through the shipped statement. It
// pins the fold: exactly one agent row, units equal to the distinct turn
// count, and a total equal to the agent rows alone.
//
// Run it against a throwaway server:
//
//	AJILAMU_LIVE_CLICKHOUSE=1 \
//	AJILAMU_LIVE_CLICKHOUSE_ADDR=127.0.0.1:18941 \
//	AJILAMU_LIVE_CLICKHOUSE_DB=t66c_r2fix \
//	AJILAMU_LIVE_CLICKHOUSE_USER=live \
//	AJILAMU_LIVE_CLICKHOUSE_PASSWORD=live-secret \
//	go test -tags live -count=1 -run TestWholePassChargesLiveFoldsAgentRows -v ./internal/ledger
func TestWholePassChargesLiveFoldsAgentRows(t *testing.T) {
	if os.Getenv(liveClickHouseOptIn) != "1" {
		t.Skipf("set %s=1 to write to a real ClickHouse", liveClickHouseOptIn)
	}
	addr := liveEnv(t, "AJILAMU_LIVE_CLICKHOUSE_ADDR")
	database := liveEnv(t, "AJILAMU_LIVE_CLICKHOUSE_DB")
	user := liveEnv(t, "AJILAMU_LIVE_CLICKHOUSE_USER")
	password := liveEnv(t, "AJILAMU_LIVE_CLICKHOUSE_PASSWORD")

	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split live address %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse live port %q: %v", portText, err)
	}

	client, err := New(&config.Config{
		ClickHouseHost:     host,
		ClickHousePort:     port,
		ClickHouseUser:     user,
		ClickHousePassword: password,
		ClickHouseDatabase: database,
	}, t.TempDir())
	if err != nil {
		t.Fatalf("open live ledger client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	liveExec(t, client, "", "CREATE DATABASE IF NOT EXISTS `"+database+"`")

	schema, err := os.ReadFile(filepath.Join("..", "..", "sql", "schema.sql"))
	if err != nil {
		t.Fatalf("read sql/schema.sql: %v", err)
	}
	for _, statement := range liveStatements(string(schema)) {
		liveExec(t, client, database, statement)
	}

	liveExec(t, client, database, liveDeleteStatement())
	t.Cleanup(func() { liveExec(t, client, database, liveDeleteStatement()) })
	liveExec(t, client, database, strings.ReplaceAll(liveSeedTemplate, "{dub}", liveDub))

	charges, err := client.WholePassCharges(context.Background(), liveDub)
	if err != nil {
		t.Fatalf("WholePassCharges: %v", err)
	}

	var agent *api.Charge
	segments := 0
	for i := range charges {
		switch charges[i].Kind {
		case "agent":
			if agent != nil {
				t.Fatalf("whole-pass charges hold more than one agent row: %+v", charges)
			}
			agent = &charges[i]
		case "segment":
			segments++
		}
	}
	if agent == nil {
		t.Fatalf("whole-pass charges %+v hold no agent row", charges)
	}
	if len(charges) != 2 || segments != 1 {
		t.Errorf("whole-pass charges = %+v, want one agent row and one segment row", charges)
	}
	if agent.Units != 2 {
		t.Errorf("agent units = %d, want the 2 distinct agent turns", agent.Units)
	}
	if agent.TotalNanodollars != cost.Price(768000) {
		t.Errorf("agent total = %d nanodollars, want 768000 from the agent rows alone", agent.TotalNanodollars)
	}
	if agent.UnitPriceNanodollars != 0 {
		t.Errorf("agent unit price = %d nanodollars, want 0", agent.UnitPriceNanodollars)
	}
	if agent.SegmentID != nil || agent.TakeFile != "" {
		t.Errorf("agent row carries take identity: %+v", *agent)
	}
	fmt.Printf("live fold: units=%d total=%d segment_rows=%d\n", agent.Units, int64(agent.TotalNanodollars), segments)
}

// liveDeleteStatement removes every row the probe seeded for liveDub.
func liveDeleteStatement() string {
	return "DELETE FROM charges_raw WHERE dub_id = '" + liveDub + "'"
}

// liveStatements splits the schema file into executable statements. Every
// semicolon in the file ends a line, so a plain split is exact.
func liveStatements(schema string) []string {
	var statements []string
	for _, chunk := range strings.Split(schema, ";") {
		if !liveHasSQL(chunk) {
			continue
		}
		statements = append(statements, strings.TrimSpace(chunk))
	}
	return statements
}

// liveHasSQL reports whether a chunk carries a statement rather than comments.
func liveHasSQL(chunk string) bool {
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		return true
	}
	return false
}

// liveExec posts one statement to the live server and fails on a non-200 reply.
func liveExec(t *testing.T, client *Client, database, statement string) string {
	t.Helper()
	endpoint := *client.endpoint
	endpoint.Path = "/"
	query := endpoint.Query()
	if database != "" {
		query.Set("database", database)
	}
	query.Set("user", client.user)
	query.Set("password", client.password)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequest(http.MethodPost, endpoint.String(), strings.NewReader(statement))
	if err != nil {
		t.Fatalf("build live request: %v", err)
	}
	response, err := client.http.Do(request)
	if err != nil {
		t.Fatalf("live request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read live reply: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("live statement failed with HTTP %d: %s\nstatement: %s", response.StatusCode, body, statement)
	}
	return string(body)
}

// liveEnv reads one required live variable and fails when it is unset.
func liveEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is unset, set every AJILAMU_LIVE_CLICKHOUSE variable", name)
	}
	return value
}
