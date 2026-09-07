# P0 close: independent verification, round 1

**Scope:** Phase 0 as landed in `b1f4275`, `351137e`, `24d3165`, `9e4fa10`.
**Method:** Independent measurement of every claim. No reliance on the completion report or on
the per-task review records.
**Verdict:** REMEDIATE. Findings: 0 C, 1 H, 1 M, 0 L.

---

## Claims that hold

Every measurement in the completion report reproduces.

| Claim | Independent check | Result |
|---|---|---|
| `seg_3_try1.wav` at 5720 ms | `ffprobe` | 5720 ms |
| `seg_3_stretched.wav` at 5338 ms | `ffprobe` | 5338 ms |
| `atempo` slows below 1.0 | Ran `atempo=0.8` on `seg_8_try1.wav` | 4200 ms to 5237 ms |
| Ratio bounds enforced | Read `ffmpeg.go:33` | Rejects outside `[0.5, 2.0]` |
| `testdata/clip.mp4` matches source | SHA256 of both files | Identical |
| Golden metrics match audio | `ffprobe` against all 10 takes | Match |
| Segment 8 carries a signed delta | Read `metrics.json` | `-2910` ms, `-40.9%` |
| Tests pass under race detection | `go test -race -count=1` | 3 packages pass |
| `.env` untracked | `git ls-files` | Untracked |
| Reference tool carries no fallback secret | Read `validate_pipeline.py:39` | Empty default |
| House style across markdown | `scratch/stylecheck.py` | 20 files, 0 violations |

`AudioFormat` exists at `probe.go:99`, which unblocks T3.1.

---

## H1: The rotated password sits in git history

**Where:** `dev-diary/adversarial-review/t0.1-round1.md` and `t0.2-round1.md`, both at HEAD and
in every commit from `24d3165` onward.

**What:** Both review records paste verification commands verbatim, and those commands embed the
old ClickHouse password as a literal. The T0.2 record claims that `git grep` finds zero password
literals in tracked files. That claim is false at HEAD. The grep matches the review file that
contains it.

**Pin:** `git grep -I "OwVeVJ" HEAD` returns three lines across two files. Expected: zero.

**Mutation:** Removing the literal from both records makes the grep clean and makes the T0.2
claim true.

**Why this matters despite rotation.** The credential is dead, so nothing is exploitable today.
Two things still argue for cleaning it now.

The repository turns public for judging. Thutapi learned this and wrote the rule into its own
protocol: sweep history before going public. History rewriting costs nothing across four
commits and grows expensive with every commit after.

A reviewer who pastes a live credential into a record has demonstrated the pattern, not the
finding. The next reviewer copies the format.

**Fix:** Redact the literal in both records, then rewrite the four commits. Record the redaction
in the files so the audit trail shows what changed.

---

## M1: Every field in `metrics.json` carries two names

**Where:** `testdata/expected/metrics.json`, all 8 records.

**What:** Each value appears twice under different keys. `target_slot_ms` and `slot_ms` hold
7110. `actual_duration_ms` and `take_ms` hold 4200. The same doubling covers
`signed_delta_ms` and `delta_ms`, `signed_delta_pct` and `delta_pct`, and `applied_repair` and
`repair`.

**Pin:** Read record 8 and count the keys. It carries five duplicated pairs.

```bash
python3 -c "import json; print(json.load(open('testdata/expected/metrics.json'))[7])"
```

**Mutation:** Collapsing each pair to one name removes the drift path.

**Why this matters.** T1.5 builds the fixture loader on this file, and P1 freezes contracts
after that. Two names for one value invites a consumer to read one key while a later edit
updates the other. The golden file exists to make segment 8 fail loudly. A golden file that can
disagree with itself cannot do that.

**Fix:** Pick one name per value and delete the alias. Prefer the names T1.1 will use, so the
fixture and the type agree.

---

## Note, not a finding

The `takes` object on each record maps a filename to a duration, which duplicates
`actual_duration_ms` for single-take segments. It carries real information for segments 3 and 4,
which hold both a try and a stretch. Leave it.
