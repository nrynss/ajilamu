# P6 close: remediation of round 7

## Role and scope

I am the REMEDIATION role for the P6 close round 7 finding. I fixed the one M. I did not
review my own work, change the verdict, tick an exit box, change a task status or commit.
My code writes stay inside the allowed paths. My only new document is this record.

## Finding row

| Finding | Fix | Pin | Mutation |
|---|---|---|---|
| M1. `internal/ledger/takes.go` `TakeAttempt.rows` wrote `Text: a.Segment.Text`, so `takes_raw.text` held the source transcript. `sql/schema.sql` documents that column as the line the take speaks after translation. `internal/ledger/workspace.go` `buildLanguageTracks` copies the column into `api.Line.Text`, which the wire documents as the target line. The Text panel target field therefore showed the source line after a reload, and a target correction reverted. | `ledger.TakeAttempt` gains a `Text` field for the spoken target line. `rows` writes `a.Text` and rejects a blank value with the other required fields. `api.RunTake` gains a `Text` field. `cmd/ajilamu/main.go` sets it from `attempt.Text` and passes `take.Text` to the ledger attempt. `internal/api/rerender.go` sets it from `rendered.Text`. | Pre-fix the seeded take stored the source line in `takes_raw.text`. The Text panel target field showed `I am an astronaut.` after a reload. Post-fix the same take stores the target line `நான் ஒரு விண்வெளி வீரர்.`, the workspace payload `languages[0].lines[0].text` equals it, and a corrected target survives a reload. `TestRecordTakeFixtureCapture` and `TestRunRecorderPersistWritesFreshRun` assert the stored target line. | Restoring `Text: a.Segment.Text` in `rows` fails `TestRecordTakeFixtureCapture` with `segment 1 text = Hi, I'm Suni Williams, want the spoken target line`. Restoring `Text: take.Segment.Text` in the recorder adapter fails `TestRunRecorderPersistWritesFreshRun` with `row field text = "hello", want "നമസ്കാരം"`. |

## What changed

1. `internal/ledger/takes.go` adds `Text string` to `TakeAttempt` and writes it into
   `takeRow.Text`. `rows` rejects a blank value beside the identity and voice fields.
2. `internal/api/run.go` adds `Text string` to `RunTake`, documented as the
   target-language line the take speaks.
3. `cmd/ajilamu/main.go` maps `attempt.Text` into the run take and `take.Text` into the
   ledger attempt.
4. `internal/api/rerender.go` maps `rendered.Text` into the re-render take.
5. `internal/ledger/takes_test.go` sets a target text in its fixture and asserts the
   stored `text`, then asserts a blank target text makes no request.
6. `cmd/ajilamu/main_test.go` sets a target text in `persistFixture` and asserts the
   `takes_raw` row carries it.
7. `internal/api/rerender_test.go` asserts the recorded re-render take carries the
   spoken target line.

`internal/api/run_test.go` was on the allowed list and needed no change. It builds
`RunTake` with only a `Segment`, and the new field is optional there.

## Measurement setup

I copied the checkout twice under `/tmp/p6r7fix-pre` and `/tmp/p6r7fix-post`. The pre-fix
copy reverts the writer and both adapters to the source text. The post-fix copy carries
the fix. I loaded a fresh ClickHouse 26.8.2.7 database from `sql/schema.sql` in container
`p6r7fix-ch` on ports 29181 and 29182, database `p6r7fix`.

Each copy ran a harness in `cmd/ajilamu` behind the `p6r7fix` tag. The harness mounts the
real `api.NewServer` with the real ledger reader, the real run recorder and a fake line
renderer. It seeds one take through the real `POST /api/dubs/{id}/run` route, which calls
the real `pipelineRunResult` and the real `runRecorder.Persist`. The seed segment is
`ta-IN` line 1 at `0..4000`, speaker `Mark Vande Hei`, source `I am an astronaut.`, target
`நான் ஒரு விண்வெளி வீரர்.`, measured 2000 ms. Headless Chrome opened the real page and
drove the real Text panel.

## Pre-fix transcript

Copy `/tmp/p6r7fix-pre`. Server on port 29184. Dub `p6r7fixpre`.

Workspace payload:

```text
GET /api/dubs/p6r7fixpre
{"line":"I am an astronaut.","takes":[1,2]}
```

The line text is the source line. Headless Chrome, Text panel for line 1:

```text
{"target":"I am an astronaut.","source":"I am an astronaut."}
```

The target field shows the source line.

Real target correction through the panel:

```text
POST /api/dubs/p6r7fixpre/lines/1/rerender
{"language":"ta-IN","text":"ta corrected target line."}
201 Line 1 re-rendered as take seg_1_try2.wav.
```

The panel showed `ta corrected target line.` immediately. A reload reset it:

```text
{"target":"I am an astronaut.","source":"I am an astronaut.",
 "takes":".../seg_1_try1.wav,.../seg_1_try2.wav"}
```

SQL `take_rates` for the dub:

```text
dub_id      segment_index  attempt  text                 chars  measured_ms  chars_per_sec
p6r7fixpre  1              1        I am an astronaut.   18     2000         9
p6r7fixpre  1              2        I am an astronaut.   18     4000         4.5
```

Both takes store the source line. The corrected target never reached the ledger.

## Post-fix transcript

Copy `/tmp/p6r7fix-post`. Server on port 29183. Dub `p6r7fixpost`.

Workspace payload after the correction:

```text
GET /api/dubs/p6r7fixpost
{"line":"ta corrected target line.","takes":[1,2]}
```

The seed read the target line `நான் ஒரு விண்வெளி வீரர்.` before the correction.

Headless Chrome, Text panel for line 1 before the correction:

```text
{"target":"நான் ஒரு விண்வெளி வீரர்.","source":"I am an astronaut."}
```

The target field shows the translated line. The source field keeps the source line.

Real target correction through the panel:

```text
POST /api/dubs/p6r7fixpost/lines/1/rerender
{"language":"ta-IN","text":"ta corrected target line."}
201 Line 1 re-rendered as take seg_1_try2.wav.
```

A reload kept it:

```text
{"target":"ta corrected target line.","source":"I am an astronaut.",
 "takes":".../seg_1_try1.wav,.../seg_1_try2.wav"}
```

SQL `take_rates` for the dub:

```text
dub_id       segment_index  attempt  text                      chars  measured_ms  chars_per_sec
p6r7fixpost  1              1        நான் ஒரு விண்வெளி வீரர்.   24     2000         12
p6r7fixpost  1              2        ta corrected target line. 25     4000         6.25
```

Both takes store the spoken target line. The corrected target reached the ledger and
survived the reload.

## Learned-prior speech rate

`take_rates.chars_per_sec` divides `lengthUTF8(text)` by `measured_ms / 1000`. The seeded
take measured 2000 ms. Pre-fix the source line gave 18 chars and 9.0 chars per second.
Post-fix the target line gave 24 chars and 12.0 chars per second. The corrected take
measured 4000 ms, so it read 4.5 before and 6.25 after.

The History tab sentence divides `measured_ms` by `slot_ms`, which the fix does not touch.
It read `0.0 percent longer ... last 1 recorded lines` on both copies.

## Mutation

I restored `Text: a.Segment.Text` in `internal/ledger/takes.go`. The scoped test failed:

```text
--- FAIL: TestRecordTakeFixtureCapture
    takes_test.go:123: segment 1 text = Hi, I'm Suni Williams, want the spoken target line
```

I restored `Text: take.Segment.Text` in the recorder adapter. The scoped test failed:

```text
--- FAIL: TestRunRecorderPersistWritesFreshRun
    main_test.go:419: row field text = "hello", want "നമസ്കാരം"
```

I restored both fixes. The same tests passed.

## Commands

| Command | Result |
|---|---|
| `go test -count=1 -run TestRecordTake ./internal/ledger/` | ok |
| `go test -count=1 -run TestRerender ./internal/api/` | ok |
| `go test -count=1 -run 'TestRunRecorder\|TestPipelineRunResult' ./cmd/ajilamu/` | ok |
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test -count=1 ./internal/... ./cmd/...` | exit 0, 14 packages ok |
| `npm --prefix web run check` | 183 files, 0 errors, 0 warnings |
| `python3 tools/audit_docs.py` | exit 0, `No drift. Docs and repository agree.` |
| `git status --short` | only the allowed paths plus this record and the earlier untracked records |

## Files changed

- `internal/ledger/takes.go`
- `internal/ledger/takes_test.go`
- `internal/api/run.go`
- `internal/api/rerender.go`
- `internal/api/rerender_test.go`
- `cmd/ajilamu/main.go`
- `cmd/ajilamu/main_test.go`

## Cleanup

I stopped both harness servers. I removed the ClickHouse container `p6r7fix-ch`. I released
both browser tabs. I deleted both copies, both data directories, both stop files, the
harness source and both mutation backups. No container, server or browser survives. The
main checkout shows only the allowed paths plus this new record and the earlier untracked
records.
