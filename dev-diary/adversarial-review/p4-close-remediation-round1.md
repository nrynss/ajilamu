# P4 close remediation, round 1

I did not change the round 1 verdict.

| # | Severity | What I changed | Measurement proving it closed |
|---|---|---|---|
| 1 | H | `RecordTake` journals the take row and every charge row, then calls `Flush` once. A 503 still returns `ErrPending`. The durable files stay until a later flush succeeds. | A local httptest returned 503 on the first take insert. Python decoded four journal files under `/tmp/p4-close-r1-remediate/artifacts/journal-after-503`. File `00000000000000000001.json` was `takes_raw` `take-8-1` `commit-drop` with `delta_ms` -2910. Files 2 through 4 were `charges_raw` for the same take and commit (`prompt_tokens`, `candidate_tokens`, `characters`). Pending was 4. After the endpoint returned 200, capture 1 delivered `takes_raw` `take-8-1` `commit-drop`. Capture 2 delivered one `charges_raw` body with three JSONEachRow lines. Every charge line contained `commit-drop`. |

The first 503 capture sent only the take body. Charge rows were already on disk. Recovery then delivered both tables.

I stayed in `internal/ledger/takes.go` and `internal/ledger/takes_test.go`.

## Validation

`gofmt -d` was empty on `internal/ledger/takes.go` and `internal/ledger/takes_test.go`.
`go test ./internal/ledger -count=1` passed.
`go test -race ./internal/ledger -count=1` passed.
`go vet ./internal/ledger` passed.
`git diff --check` passed.

I did not tick PHASE-4 exit boxes. I did not edit the review file. I did not commit.
