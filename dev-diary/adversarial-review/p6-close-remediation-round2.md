# P6 close remediation, round 2

I did not change the round 2 verdict. I fixed finding M1 and nothing else.

I edited `internal/api/mutations.go`, `internal/api/mutations_test.go` and
`web/src/routes/d/[id]/+page.svelte`. I did not commit.

| Finding | Fix | Pin | Residue |
|---|---|---|---|
| M1, the Boundary panel's Keep overlap confirmation can never save. `boundaryEntry` rejected every overlap, so the route answered 400 for the confirmed bounds. T6.1 requires an explicit confirmation to permit an overlap. | `EditBody` gains `allow_overlap`. `boundaryEntry` skips the neighbour check only when the creator confirms, and it still rejects a silent overlap. The page marks the confirmed case, because only the Keep overlap control emits an overlapping change. `recordEdit` sends the flag for that change alone. | A committed test pins both paths. A live nudge on the served page sent `allow_overlap` and got 201, one commit, one action and one snapshot. Outside SQL read the overlapping bounds, and a reload kept them. A silent overlap answered 400. | None. The route still rejects a silent overlap. `Boundary.svelte` did not change, so its collision confirm still gates the overlap. The `boundaryEntry` comment now describes the confirmed path. |

## Finding 1 measurements

I ran the fixed binary against ClickHouse 26.8.2.7 on port 8124. I seeded a
fresh dub, `p6r2rem`, with one root commit and three timeline rows.

### A confirmed overlap saves

I drove the served page in headless Chromium on the rebuilt bundle. A keyboard
nudge on line 1's end handle opened the collision confirm. A real click on
Keep overlap sent this request.

```text
{"kind":"boundary","language":"ml-IN","segment_id":1,"start_ms":0,"end_ms":5050,"allow_overlap":true}
```

The route answered 201.

```text
{"commit_id":"f591530125d1d2a2780dc110bd13881c","action":"boundary_nudged","author":"manual_ui","segment":{"id":1,"start_ms":0,"end_ms":5050,"duration_ms":5050,"speaker":"Mark"},"sentence":"Saved the boundary. Line 1 now runs from 0.000s to 5.050s."}
```

Outside SQL read one commit, one action and one snapshot for that commit. Line
2 still runs 3750 to 8250, so the stored bounds carry the overlap.

```text
commit_id                           parent_commit_id                    version_seq  language  message
rem-c1                                                                  1           ml-IN     Segmented the film.
c57afc5badb331be08f6c2743473a00c    rem-c1                              2           ml-IN     Nudged the boundary of line 1.
f591530125d1d2a2780dc110bd13881c    c57afc5badb331be08f6c2743473a00c   3           ml-IN     Nudged the boundary of line 1.

commit_id                           segment_index  action_type      author     prompt  before_value                                    after_value
f591530125d1d2a2780dc110bd13881c    1              boundary_nudged  manual_ui          {"start_ms":0,"end_ms":5000,"speaker":"Mark"}    {"start_ms":0,"end_ms":5050,"speaker":"Mark"}

commit_id                           version_seq  segment_index  start_ms  end_ms  speaker  source_text  text  take_id
f591530125d1d2a2780dc110bd13881c    3            1              0         5050    Mark     one          onnu  t1

commits  actions  snapshots
1        1        1
```

After a reload the Boundary panel read `0:00.000 to 0:05.050`. The workspace
payload served the same bounds, and line 1 overlaps line 2.

```text
[
  {"id": 1, "start_ms": 0, "end_ms": 5050, "speaker": "Mark"},
  {"id": 2, "start_ms": 3750, "end_ms": 8250, "speaker": "Suni"},
  {"id": 3, "start_ms": 9000, "end_ms": 12000, "speaker": "Maya"}
]
```

The timeline route served the same head bounds.

```text
[
  {"segment_index": 1, "start_ms": 0, "end_ms": 5050},
  {"segment_index": 2, "start_ms": 3750, "end_ms": 8250},
  {"segment_index": 3, "start_ms": 9000, "end_ms": 12000}
]
```

### A silent overlap still fails

The same bounds without the flag answered 400.

```text
status=400
{"error":"That boundary would overlap another line."}
```

### Committed test

`TestEditBoundaryOverlapNeedsConfirmation` pins both paths. The silent subtest
wants 400, the overlap sentence and zero recorder calls. The confirmed subtest
wants 201, one recorder call and the overlapping snapshot.

```text
=== RUN   TestEditBoundaryOverlapNeedsConfirmation
=== RUN   TestEditBoundaryOverlapNeedsConfirmation/silent_overlap_fails
=== RUN   TestEditBoundaryOverlapNeedsConfirmation/confirmed_overlap_saves
--- PASS: TestEditBoundaryOverlapNeedsConfirmation (0.00s)
ok  	github.com/nrynss/ajilamu/internal/api	0.006s
```

### Mutation

Mutation A restores the blanket rejection. I changed `if !allowOverlap {` to
`if true {`. The confirmed subtest fails.

```text
--- FAIL: TestEditBoundaryOverlapNeedsConfirmation (0.00s)
    --- FAIL: TestEditBoundaryOverlapNeedsConfirmation/confirmed_overlap_saves (0.00s)
        mutations_test.go:261: confirmed overlap status = 400, want 201
FAIL
```

Mutation B ignores the flag. I changed `if !allowOverlap {` to `if false {`.
The silent subtest fails.

```text
--- FAIL: TestEditBoundaryOverlapNeedsConfirmation (0.00s)
    --- FAIL: TestEditBoundaryOverlapNeedsConfirmation/silent_overlap_fails (0.00s)
        mutations_test.go:239: overlap status = 201, want 400
FAIL
```

I restored the guard after each mutation.

## Required validation

`go test -count=1 ./internal/api ./cmd/...`

```text
ok  	github.com/nrynss/ajilamu/internal/api	3.951s
ok  	github.com/nrynss/ajilamu/cmd/ajilamu	4.327s
```

`npm --prefix web run check`

```text
COMPLETED 183 FILES 0 ERRORS 0 WARNINGS 0 FILES_WITH_PROBLEMS
```

I did not tick any PHASE-6 exit box. I did not edit the round 2 review file.
