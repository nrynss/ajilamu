# P6 Merge Round 1 Review

## Verdict

**APPROVE**

| C | H | M | L |
|---|---:|---:|---:|---:|
| 0 | 0 | 0 | 0 |

## Scope and Evidence

I read `dev-diary/PHASE-6-editing.md`, `dev-diary/adversarial-review/t6.1-round3.md`, and `dev-diary/adversarial-review/t6.4-round2.md`. I read the three resolved files in full. I read merge commit `8b6a9a5` against both parents with `git diff` and `git show --cc`.

Nothing lost. I compared every added line from each parent against the merged file with `grep -Fxv -f <merged>`. `internal/api/server.go` lost no line from either parent. The workspace page lost no line from master. The page lost three branch lines only, the fixture effect lines that master's reviewed local variable fix replaces.

```
          const fixtureDub = loadFixtureDub(id ?? "fixture")
          dub = fixtureDub
          workspaceState = fixtureDub && fixtureDub.segments.length > 0 && fixtureDub.languages.length > 0
```

The merged file carries master's replacement, `const loaded = loadFixtureDub(id ?? "fixture")`. The merge commit message names this change. I found no other dropped line.

The mux kept every route master mounts and added the preview route. I exercised each route over HTTP against the merged build.

| Route | Result |
|---|---|
| `GET /api/healthz` | 200 |
| `GET /api/ledger/ready` | 200 |
| `GET /api/dubs` | 200 |
| `GET /api/dubs/fixture/history` | 200 |
| `GET /api/dubs/fixture/timeline` | 400, language required |
| `GET /api/dubs/fixture/branches` | 400, language required |
| `POST /api/dubs/fixture/run` | 404, no source video |
| `POST /api/dubs/fixture/run/cancel` | 409, no active run |
| `GET /api/dubs/fixture/events` | 404, no run |
| `POST /api/editor/commands/preview` | 200 |
| `GET /api/nope` | 404 |

Browser proof. I built `HEAD` with `go build -o /tmp/ajilamu-merge-review ./cmd/ajilamu`. I ran it on `127.0.0.1:18777` with `ENV=development` and `AJILAMU_FRONTEND_DIR=web/build`. I drove `web/build` in headless Chromium through Playwright.

| Check | Measurement |
|---|---|
| T6.1 control | `/d/fixture` exposed `Adjust boundary for line 1` with two `role=slider` handles. |
| T6.1 keyboard | ArrowRight moved the line 2 start from 7728 to 7778. The route timeline `data-start-ms` followed. |
| T6.1 pointer | A pointer drag moved the line 2 start to 8820. The route timeline followed. |
| T6.1 length bar | The Line 2 length bar read `0:05.480`, then `0:05.430` after the key, then `0:04.388` after the drag. |
| T6.4 focus | `/` focused `editor-command` and typed no slash. |
| T6.4 preview | `shorten line 4 by 0.5s.` returned `200 POST /api/editor/commands/preview` and showed `Shorten line 4 by 0.500s.` with Confirm change and Revise. |
| T6.4 apply | Confirm changed the line 4 `data-duration-ms` from 5660 to 5160. |
| History DAG | The History tab drew 7 nodes and 6 edges, versions Saved 1 through Saved 7. |
| Tab switching | Lines, Details, and History panels switched. They added zero console errors. |
| Run start | The button produced `This project has no source video, so the run cannot start.` with `role=status`. The sentence ends in a period. |

Suites. All passed.

| Command | Result |
|---|---|
| `go build ./...` | exit 0 |
| `go test -count=1 ./internal/api ./cmd/...` | ok, 1.470s and 1.012s |
| `go test -count=1 ./internal/fit` | ok, 17.486s |
| `npm --prefix web run check` | 180 files, 0 errors, 0 warnings |
| `npm --prefix web run build` | built in 2.26s |
| `python3 tools/audit_docs.py` | No drift |

Svelte hygiene. The page, `Boundary.svelte`, and `CommandBar.svelte` contain no `export let`, no `$:` label, and no `svelte/store` import.

Board. `P6` reads `2 / 6`. `PHASE-6-editing.md` marks T6.1 and T6.4 done and leaves four tasks not started. `P7` reads `14 / 18`. `PHASE-7-ingestion.md` marks 14 done and leaves T7.4a, T7.5, T7.5a, and T7.6 not started. Both counts match.

## Findings

Zero findings.

| # | Sev | Where | What | Pin | Mutation |
|---|---|---|---|---|---|
| None | | | No defects found. | The measurements above passed. | No mutation is required. |

## Residue against the task reviews

T6.1 was APPROVE on the branch and stays APPROVE on the merged tree. The boundary control, the keyboard nudge, the pointer drag, and the length bar all work. The merge did not change `Boundary.svelte`.

T6.4 was APPROVE on the branch and stays APPROVE on the merged tree. The command bar focuses on `/`, previews the documented command through the server route, and applies the Go-derived value on Confirm. The merge did not change `CommandBar.svelte`, `internal/command`, or `internal/api/command.go`.

## What I did not file

The fixture run start logs one console error when the server answers 404. Master's T7.3a code owns that path, and the merge did not change `startRun` or the run route. The page shows a complete sentence, so the demo survives.

The merge removes master's placeholder command form. The branch replaces that stub with the working `CommandBar`. Master's form only set a status sentence, so the removal loses no working behavior.

The page drops three branch fixture-effect lines. Master's reviewed local variable fix replaces them, and the merge commit message records the choice.

The `P1` row reads `5 / 6` and the `P7` row text comes from master. Both match their phase files, and this task only asked for `P6` and `P7`.
