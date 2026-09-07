# P0 Close Remediation: Round 1

We remediated all findings from the round 1 review. We report zero residue against findings H1 and M1.

| Finding | Where | What | Remediation | Verification |
|---|---|---|---|---|
| H1 | `dev-diary/adversarial-review/t0.1-round1.md`, `t0.2-round1.md` | Rotated password literal appeared in review records | Redacted literal to `[REDACTED_OLD_PASSWORD]` and added audit notes | Credential grep returned zero matches |
| M1 | `testdata/expected/metrics.json`, `testdata/fixtures_test.go` | Duplicate alias keys existed across all records | Deleted alias keys and updated `ExpectedMetric` struct | `go test -race -count=1 -v ./testdata` passed |

## Remediation Details

### Finding H1: Rotated password in review records

- **Where:** `dev-diary/adversarial-review/t0.1-round1.md` and `dev-diary/adversarial-review/t0.2-round1.md`.
- **What:** Both files pasted verification commands containing the old ClickHouse password literal.
- **Remediation:** We replaced the old password literal with `[REDACTED_OLD_PASSWORD]` in both review files. We appended an audit trail section to each file explaining the redaction.
- **Verification:** We ran the credential search command across the working tree. The command returned zero matches.

### Finding M1: Duplicate alias keys in metrics.json

- **Where:** `testdata/expected/metrics.json` and `testdata/fixtures_test.go`.
- **What:** Every record in `metrics.json` duplicated values under two distinct key names.
- **Remediation:** We removed all duplicate alias keys across all eight records in `testdata/expected/metrics.json`. We preserved only canonical keys. We updated `ExpectedMetric` in `testdata/fixtures_test.go` to match the canonical schema.
- **Verification:** We executed `go test -race -count=1 -v ./testdata`. All three test functions passed cleanly under the race detector.
