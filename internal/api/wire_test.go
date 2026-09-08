package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/cost"
)

// resolveWire locates an example payload across working directory layouts.
func resolveWire(t *testing.T, name string) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "testdata", "wire", name),
		filepath.Join("testdata", "wire", name),
		filepath.Join("..", "testdata", "wire", name),
		filepath.Join("..", name),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("wire example not found: %s", name)
	return ""
}

// readExample loads one example payload from testdata/wire.
func readExample(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(resolveWire(t, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// decodeExample unmarshals one example payload into dst.
func decodeExample(t *testing.T, name string, dst any) {
	t.Helper()
	if err := json.Unmarshal(readExample(t, name), dst); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
}

// wireExample pairs one example payload with the struct it decodes into.
type wireExample struct {
	// name is the example file name under testdata/wire.
	name string
	// decode returns a fresh pointer to the target struct.
	decode func() any
}

// wireExamples lists every example payload and its target struct.
// Each shared struct in wire.go appears here at least once.
var wireExamples = []wireExample{
	{"dub.json", func() any { return &Dub{} }},
	{"dubs.json", func() any { return &DubIndex{} }},
	{"dub_summary.json", func() any { return &DubSummary{} }},
	{"segment.json", func() any { return &Segment{} }},
	{"language_track.json", func() any { return &LanguageTrack{} }},
	{"line.json", func() any { return &Line{} }},
	{"take.json", func() any { return &Take{} }},
	{"fit.json", func() any { return &Fit{} }},
	{"charge.json", func() any { return &Charge{} }},
	{"total.json", func() any { return &Total{} }},
	{"commit.json", func() any { return &Commit{} }},
	{"dub_history.json", func() any { return &DubHistory{} }},
	{"timeline_view.json", func() any { return &TimelineView{} }},
	{"timeline_entry.json", func() any { return &TimelineEntry{} }},
	{"branch_comparison.json", func() any { return &BranchComparison{} }},
	{"branch_summary.json", func() any { return &BranchSummary{} }},
	{"progress.json", func() any { return &ProgressEvent{} }},
	{"language_catalog.json", func() any { return &LanguageCatalog{} }},
}

// TestExamplesUnmarshal proves every example payload parses into its struct.
func TestExamplesUnmarshal(t *testing.T) {
	for _, sample := range wireExamples {
		t.Run(sample.name, func(t *testing.T) {
			dst := sample.decode()
			decodeExample(t, sample.name, dst)
			if _, err := json.Marshal(dst); err != nil {
				t.Fatalf("marshal %s: %v", sample.name, err)
			}
		})
	}
}

// TestExamplesMatchJSONTags proves every example key matches a Go JSON tag.
// encoding/json ignores unknown keys, so a renamed key would decode silently.
func TestExamplesMatchJSONTags(t *testing.T) {
	for _, sample := range wireExamples {
		t.Run(sample.name, func(t *testing.T) {
			var raw any
			if err := json.Unmarshal(readExample(t, sample.name), &raw); err != nil {
				t.Fatalf("decode %s as generic JSON: %v", sample.name, err)
			}
			typ := reflect.TypeOf(sample.decode()).Elem()
			checkJSONKeys(t, raw, typ, sample.name)
		})
	}
}

// checkJSONKeys walks a decoded example against its Go type.
// It fails on a key that no tag names and on a missing required key.
func checkJSONKeys(t *testing.T, raw any, typ reflect.Type, path string) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		items, ok := raw.([]any)
		if !ok {
			t.Errorf("%s: want an array, got %T", path, raw)
			return
		}
		for i, item := range items {
			checkJSONKeys(t, item, typ.Elem(), fmt.Sprintf("%s[%d]", path, i))
		}
	case reflect.Struct:
		object, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("%s: want an object, got %T", path, raw)
			return
		}
		fields := map[string]reflect.StructField{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			key := strings.Split(field.Tag.Get("json"), ",")[0]
			if key == "" || key == "-" {
				continue
			}
			fields[key] = field
		}
		for key := range object {
			if _, ok := fields[key]; !ok {
				t.Errorf("%s: key %q matches no Go JSON tag", path, key)
			}
		}
		for key, field := range fields {
			value, present := object[key]
			if !present {
				if !strings.Contains(field.Tag.Get("json"), "omitempty") {
					t.Errorf("%s: required key %q is absent", path, key)
				}
				continue
			}
			if value == nil {
				continue
			}
			checkJSONKeys(t, value, field.Type, path+"."+key)
		}
	}
}

// TestEverySharedStructHasExample proves each mirrored type has an example.
func TestEverySharedStructHasExample(t *testing.T) {
	covered := map[string]bool{}
	for _, sample := range wireExamples {
		covered[reflect.TypeOf(sample.decode()).Elem().Name()] = true
	}
	for _, sample := range mirroredTypes {
		name := reflect.TypeOf(sample).Name()
		if !covered[name] {
			t.Errorf("shared struct %s has no example payload", name)
		}
	}
}

// mirroredTypes lists every struct shared between wire.go and types.ts.
// The parity test fails when a struct is added on one side only.
var mirroredTypes = []any{
	DubIndex{},
	DubSummary{},
	Dub{},
	LanguageTrack{},
	Line{},
	Take{},
	Fit{},
	Segment{},
	Charge{},
	Total{},
	Commit{},
	ProgressEvent{},
	DubHistory{},
	TimelineView{},
	TimelineEntry{},
	BranchComparison{},
	BranchSummary{},
	LanguageCatalog{},
}

// parseWireStructs extracts every exported struct name from wire.go.
// It follows parseTSInterfaces, which parses types.ts line by line.
func parseWireStructs(t *testing.T, src string) []string {
	t.Helper()
	decl := regexp.MustCompile(`^type ([A-Z][A-Za-z0-9_]*) struct \{`)
	var names []string
	for _, line := range strings.Split(src, "\n") {
		if m := decl.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	if len(names) == 0 {
		t.Fatal("wire.go declares no exported struct")
	}
	return names
}

// TestEveryWireStructIsRegistered derives the shared set from wire.go.
// A new struct must reach mirroredTypes and wireExamples or this test fails.
func TestEveryWireStructIsRegistered(t *testing.T) {
	src, err := os.ReadFile("wire.go")
	if err != nil {
		t.Fatalf("read wire.go: %v", err)
	}
	names := parseWireStructs(t, string(src))

	mirrored := map[string]bool{}
	for _, sample := range mirroredTypes {
		mirrored[reflect.TypeOf(sample).Name()] = true
	}
	covered := map[string]bool{}
	for _, sample := range wireExamples {
		covered[reflect.TypeOf(sample.decode()).Elem().Name()] = true
	}
	for _, name := range names {
		if !mirrored[name] {
			t.Errorf("wire.go struct %s is absent from mirroredTypes", name)
		}
		if !covered[name] {
			t.Errorf("wire.go struct %s has no wireExamples entry", name)
		}
	}
}

// tsInterface holds the parsed properties of one TypeScript interface.
type tsInterface struct {
	// optional reports whether each property is optional.
	optional map[string]bool
}

// parseTSInterfaces extracts interface property names from types.ts.
func parseTSInterfaces(t *testing.T, src string) map[string]tsInterface {
	t.Helper()
	interfaces := map[string]tsInterface{}
	begin := regexp.MustCompile(`^export interface ([A-Za-z_][A-Za-z0-9_]*) \{`)
	property := regexp.MustCompile(`^[ \t]*([A-Za-z_][A-Za-z0-9_]*)(\?)?[ \t]*:`)
	current := ""
	parsed := tsInterface{optional: map[string]bool{}}
	for _, line := range strings.Split(src, "\n") {
		if m := begin.FindStringSubmatch(line); m != nil {
			current = m[1]
			parsed = tsInterface{optional: map[string]bool{}}
			continue
		}
		if current == "" {
			continue
		}
		if strings.TrimSpace(line) == "}" {
			interfaces[current] = parsed
			current = ""
			continue
		}
		if m := property.FindStringSubmatch(line); m != nil {
			parsed.optional[m[1]] = m[2] == "?"
		}
	}
	if current != "" {
		t.Fatalf("types.ts ends inside interface %s", current)
	}
	return interfaces
}

// TestGoTSParity proves Go JSON tags equal TypeScript property keys.
// The same pass checks that optionality matches omitempty on each field.
func TestGoTSParity(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "types.ts"))
	if err != nil {
		t.Fatalf("read types.ts: %v", err)
	}
	ts := parseTSInterfaces(t, string(src))
	for _, sample := range mirroredTypes {
		typ := reflect.TypeOf(sample)
		name := typ.Name()
		want, ok := ts[name]
		if !ok {
			t.Errorf("types.ts lacks interface %s", name)
			continue
		}
		goKeys := map[string]bool{}
		goOptional := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag := field.Tag.Get("json")
			if tag == "" {
				t.Errorf("%s field %s lacks a json tag", name, field.Name)
				continue
			}
			parts := strings.Split(tag, ",")
			key := parts[0]
			if key == "" {
				t.Errorf("%s field %s has an empty json key", name, field.Name)
				continue
			}
			goKeys[key] = true
			goOptional[key] = strings.Contains(tag, "omitempty")
		}
		var missing, extra, optionalMismatch []string
		for key := range goKeys {
			optional, ok := want.optional[key]
			if !ok {
				missing = append(missing, key)
				continue
			}
			if optional != goOptional[key] {
				optionalMismatch = append(optionalMismatch, key)
			}
		}
		for key, optional := range want.optional {
			if _, ok := goKeys[key]; !ok {
				extra = append(extra, key)
				continue
			}
			if optional != goOptional[key] {
				optionalMismatch = append(optionalMismatch, key)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		sort.Strings(optionalMismatch)
		if len(missing) > 0 {
			t.Errorf("%s: Go tags missing from types.ts: %v", name, missing)
		}
		if len(extra) > 0 {
			t.Errorf("%s: types.ts keys missing from Go tags: %v", name, extra)
		}
		if len(optionalMismatch) > 0 {
			t.Errorf("%s: optionality differs from omitempty: %v", name, optionalMismatch)
		}
	}
}

// fitStateSet lists the states the wire contract permits.
var fitStateSet = map[string]bool{
	StateFits:     true,
	StateTooLong:  true,
	StateTooShort: true,
}

// walkTakes visits every take across every language line of a dub.
func walkTakes(t *testing.T, d *Dub, visit func(line Line, take Take)) {
	t.Helper()
	for _, track := range d.Languages {
		for _, line := range track.Lines {
			for _, take := range line.Takes {
				visit(line, take)
			}
		}
	}
}

// TestFitStatesMatchDomainSemantics checks every stated fit against FitState.
// A short take can never serialize as fits.
func TestFitStatesMatchDomainSemantics(t *testing.T) {
	var dub Dub
	decodeExample(t, "dub.json", &dub)
	var take Take
	decodeExample(t, "take.json", &take)
	checks := []Fit{dub.Languages[0].Lines[7].Takes[0].Fit, take.Fit}
	var fit Fit
	decodeExample(t, "fit.json", &fit)
	checks = append(checks, fit)
	walkTakes(t, &dub, func(_ Line, candidate Take) {
		checks = append(checks, candidate.Fit)
	})
	for _, f := range checks {
		if !fitStateSet[f.State] {
			t.Errorf("unknown fit state %q", f.State)
		}
		if f.DeltaMs != f.MeasuredMs-f.SlotMs {
			t.Errorf("delta %d does not equal measured %d minus slot %d", f.DeltaMs, f.MeasuredMs, f.SlotMs)
		}
		if want := FitState(f.SlotMs, f.MeasuredMs); f.State != want {
			t.Errorf("state %q for delta %d, want %q", f.State, f.DeltaMs, want)
		}
	}
}

// TestShortTakeExampleNeverReportsFits pins the segment 8 regression.
func TestShortTakeExampleNeverReportsFits(t *testing.T) {
	var fit Fit
	decodeExample(t, "fit.json", &fit)
	if fit.SlotMs != 7110 || fit.MeasuredMs != 4200 || fit.DeltaMs != -2910 {
		t.Fatalf("short fit example wrong: %+v", fit)
	}
	if fit.State != StateTooShort || fit.State == StateFits {
		t.Fatalf("short take serialized as %q, want too_short", fit.State)
	}
	var dub Dub
	decodeExample(t, "dub.json", &dub)
	line := dub.Languages[0].Lines[7]
	if line.Takes[0].Fit.DeltaMs != -2910 {
		t.Fatalf("segment 8 delta %d, want -2910", line.Takes[0].Fit.DeltaMs)
	}
	if line.Takes[0].Fit.State == StateFits {
		t.Fatal("segment 8 take serialized as fits")
	}
	if !line.Flagged {
		t.Fatal("segment 8 line should be flagged for review")
	}
}

// TestMicroCostSurvivesAsInteger pins the T1.2 round one defect.
// A sub-cent charge must arrive as an exact nonzero integer.
func TestMicroCostSurvivesAsInteger(t *testing.T) {
	var charge Charge
	decodeExample(t, "charge.json", &charge)
	if charge.Kind != ChargeTranslate {
		t.Fatalf("example kind %q, want %q", charge.Kind, ChargeTranslate)
	}
	if got := charge.Units * int64(charge.UnitPriceNanodollars); got != int64(charge.TotalNanodollars) {
		t.Fatalf("total %d does not equal units %d times price %d", charge.TotalNanodollars, charge.Units, charge.UnitPriceNanodollars)
	}
	if charge.TotalNanodollars <= 0 {
		t.Fatalf("micro charge vanished to zero")
	}
	if charge.TotalNanodollars >= cost.Cent {
		t.Fatalf("example should stay below one cent, got %d", charge.TotalNanodollars)
	}
	data, err := json.Marshal(charge)
	if err != nil {
		t.Fatalf("marshal charge: %v", err)
	}
	money := regexp.MustCompile(`"(unit_price_nanodollars|total_nanodollars)":(-?[0-9]+)`)
	matches := money.FindAllStringSubmatch(string(data), -1)
	if len(matches) != 2 {
		t.Fatalf("money did not serialize as integers: %s", data)
	}
}

// TestProgressEventsCarrySentenceAndCost pins the SSE envelope contract.
func TestProgressEventsCarrySentenceAndCost(t *testing.T) {
	var event ProgressEvent
	decodeExample(t, "progress.json", &event)
	if event.Type != EventProgress {
		t.Fatalf("event type %q, want progress", event.Type)
	}
	if !strings.Contains(event.Sentence, " ") || strings.TrimSpace(event.Sentence) == "" {
		t.Fatalf("event sentence missing: %q", event.Sentence)
	}
	if event.TotalNanodollars < 0 {
		t.Fatalf("cumulative cost negative: %d", event.TotalNanodollars)
	}
	if event.Stage == "" {
		t.Fatal("event lacks a stage reference")
	}
}

// TestCommitDAGFieldsPresent pins the history rail payload contract.
func TestCommitDAGFieldsPresent(t *testing.T) {
	var commit Commit
	decodeExample(t, "commit.json", &commit)
	if commit.CommitID == "" || commit.ParentCommitID == "" {
		t.Fatalf("commit chain example lacks DAG identity: %+v", commit)
	}
	if commit.VersionNumber < 1 {
		t.Fatalf("version number %d", commit.VersionNumber)
	}
	if _, err := time.Parse(time.RFC3339, commit.CreatedAt); err != nil {
		t.Fatalf("created_at is not RFC 3339: %v", err)
	}
	if commit.Action == "" || commit.Author == "" {
		t.Fatalf("commit lacks action metadata: %+v", commit)
	}
	if commit.Action == ActionUserCommand && commit.Instruction == "" {
		t.Fatal("user_command commit dropped its original instruction")
	}

	var dub Dub
	decodeExample(t, "dub.json", &dub)
	if len(dub.Commits) < 2 {
		t.Fatalf("dub commits chain too short: %d", len(dub.Commits))
	}
	for i, c := range dub.Commits {
		if c.VersionNumber != i+1 {
			t.Fatalf("commit %d version %d, want %d", i, c.VersionNumber, i+1)
		}
		if _, err := time.Parse(time.RFC3339, c.CreatedAt); err != nil {
			t.Fatalf("commit %d created_at: %v", i, err)
		}
		if i == 0 {
			if c.ParentCommitID != "" {
				t.Fatalf("root commit has a parent: %q", c.ParentCommitID)
			}
			continue
		}
		if c.ParentCommitID != dub.Commits[i-1].CommitID {
			t.Fatalf("commit %d parent %q does not link commit %d", i, c.ParentCommitID, i-1)
		}
	}
}

// TestDubTotalReconcilesCharges proves the dub total counts every charge once.
func TestDubTotalReconcilesCharges(t *testing.T) {
	var dub Dub
	decodeExample(t, "dub.json", &dub)
	var sum cost.Price
	addCharge := func(c Charge) {
		exact := cost.Price(c.Units) * c.UnitPriceNanodollars
		if c.TotalNanodollars != exact {
			t.Fatalf("charge %s total %d, want %d", c.TakeFile, c.TotalNanodollars, exact)
		}
		if c.TotalNanodollars <= 0 {
			t.Fatalf("charge total not positive: %d", c.TotalNanodollars)
		}
		if !isChargeKind(c.Kind) {
			t.Fatalf("unknown charge kind %q", c.Kind)
		}
		sum += c.TotalNanodollars
	}
	for _, c := range dub.Charges {
		addCharge(c)
	}
	walkTakes(t, &dub, func(_ Line, take Take) {
		if !isRepairKind(take.Repair) {
			t.Fatalf("take %s has unknown repair %q", take.File, take.Repair)
		}
		if (take.Repair == RepairAtempo) != (take.StretchFactorMilli > 0) {
			t.Fatalf("take %s repair %q conflicts with stretch factor %d", take.File, take.Repair, take.StretchFactorMilli)
		}
		for _, c := range take.Charges {
			addCharge(c)
		}
	})
	if dub.Total.TotalNanodollars != sum {
		t.Fatalf("total %d does not match charges %d", dub.Total.TotalNanodollars, sum)
	}
	if strings.TrimSpace(dub.Total.Covers) == "" {
		t.Fatal("total does not state what it covers")
	}
}

// TestDubSummariesWellFormed pins the index route payload.
func TestDubSummariesWellFormed(t *testing.T) {
	var index DubIndex
	decodeExample(t, "dubs.json", &index)
	if len(index.Dubs) == 0 {
		t.Fatal("index example lists no dubs")
	}
	for _, summary := range index.Dubs {
		if summary.ID == "" || summary.Title == "" {
			t.Fatalf("summary lacks identity: %+v", summary)
		}
		if summary.TotalNanodollars < 0 {
			t.Fatalf("summary total negative: %d", summary.TotalNanodollars)
		}
		if _, err := time.Parse(time.RFC3339, summary.CreatedAt); err != nil {
			t.Fatalf("summary created_at: %v", err)
		}
		if _, err := time.Parse(time.RFC3339, summary.UpdatedAt); err != nil {
			t.Fatalf("summary updated_at: %v", err)
		}
	}
}

// isChargeKind reports whether kind is a known charge kind.
func isChargeKind(kind string) bool {
	switch kind {
	case ChargeSegment, ChargeTranslate, ChargeSynthesize,
		ChargeAgent:
		return true
	}
	return false
}

// isRepairKind reports whether repair is a known repair kind.
func isRepairKind(repair string) bool {
	switch repair {
	case RepairNone, RepairAtempo, RepairRewrite, RepairManual:
		return true
	}
	return false
}

// TestChargeKindGuardCoversWireConstants keeps the guard and the constants together.
// A new charge kind must reach isChargeKind or the example walk fails.
func TestChargeKindGuardCoversWireConstants(t *testing.T) {
	for _, kind := range []string{ChargeSegment, ChargeTranslate, ChargeSynthesize, ChargeAgent} {
		if !isChargeKind(kind) {
			t.Errorf("isChargeKind(%q) = false, want true", kind)
		}
	}
	if isChargeKind("unknown") {
		t.Error("isChargeKind(unknown) = true, want false")
	}
}

// TestChargeKindParity proves the wire constants match the TypeScript union.
// The three declarations must move together, or the UI drops the new kind.
func TestChargeKindParity(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "types.ts"))
	if err != nil {
		t.Fatalf("read types.ts: %v", err)
	}
	decl := regexp.MustCompile(`export type ChargeKind = ([^\n]+)`)
	match := decl.FindStringSubmatch(string(src))
	if match == nil {
		t.Fatal("types.ts declares no ChargeKind union")
	}
	ts := map[string]bool{}
	for _, part := range strings.Split(match[1], "|") {
		ts[strings.Trim(strings.TrimSpace(part), `"`)] = true
	}
	want := []string{ChargeSegment, ChargeTranslate, ChargeSynthesize, ChargeAgent}
	for _, kind := range want {
		if !ts[kind] {
			t.Errorf("types.ts ChargeKind lacks %q", kind)
		}
	}
	if len(ts) != len(want) {
		t.Errorf("types.ts ChargeKind has %d members, want %d", len(ts), len(want))
	}
}
