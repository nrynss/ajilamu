# P8 close review, round 1

## Scope and role

I am the round one close reviewer for P8 Ship. I fix nothing. My only repository write is this
record. The phase deliberately ran without a per task review loop, so no round tested T8.1, T8.2
or T8.4 against a specification before this close. I judge the phase on measured behaviour rather
than on the absence of rounds. The checkout is branch `master` at HEAD `ec40dbc` with a clean tree
on entry. I ran `python3 tools/audit_docs.py` first and it exited 0 with no drift.

## Method

I measured the live host at `https://ajilamu.nryn.dev`. I drove one sample run end to end and
followed its event stream to the terminal event. I queried the ClickHouse Cloud ledger as the
writer through the repository `.env`. I decoded the export with `ffmpeg` and `ffprobe` and fitted
the ducking with least squares. I read `deploy/`, `README.md`, the submission, the runbook and
every P8 and T8.x record. I did not read host logs. I did not destroy or rebuild the host. All
local evidence sits under `/tmp/p8close-live`.

## Verdict

**REMEDIATE.**

| Severity | Count |
|---|---:|
| C | 0 |
| H | 2 |
| M | 3 |
| L | 2 |

The demo runs. The two H findings attack the honest instrument claim rather than the run.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/ledger/workspace.go:22-36`, `web/src/lib/tabs/DetailsTab.svelte:98`, `web/src/routes/d/[id]/+page.svelte:208-216` | The workspace attaches charges only to the chosen take, because the `takes` view holds one row per line. The Details tab reports the chosen attempt's cost as the line cost across every try, and the flagged copy reports one try for a line that rendered three. | Dub `b82693911467eae901626876e4a045a2` segment 1 holds 4,596,450 nanodollars across 3 attempts, and the live tab reads `This line has cost $0.00073905 across every try`. Dub `38fa4e9c69715c3c3b2d011a14d9eccf` segment 7 holds 11,398,200, and the tab reads `$0.00366765`. The workspace payload attaches 23,761,200 of 53,571,300 for the first dub and 23,787,150 of 65,330,550 for the second. Line 6 rendered 3 takes and the copy reads `after 1 try`. | Shipping only the chosen take's charges hides most of a line's spend, so the demo's line cost claim is false. |
| `docs/submission.md:83-85`, `docs/demo-runbook.md:53` | The submission says `the workspace can show what one line cost across every try`. The runbook tells the operator to read `$0.00366765` as that cost. The workspace shows neither. | The two ledger sums above. The project total is correct and the per line total is not. | A judge reads a number that omits two of three attempts, so the honest instrument argument fails on its own evidence. |
| `README.md:76`, `docs/src/content/docs/index.mdx:31,50`, `docs/src/content/docs/architecture/fit-loop.md:29` | The public docs say the system time stretches within 8 percent either way. The code uses 5 percent on the short side. | `DefaultMaxStretchLong = 0.08` at `internal/fit/stretch.go:30` and `DefaultMaxStretchShort = 0.05` at line 47. `docs/submission.md:52` states 5 percent. | A take 6 percent short is rewritten for a fee rather than stretched, so the docs misstate cost and behaviour. |
| `README.md:225-228`, `docs/src/content/docs/reference/limitations.md:33-36` | Both still say the live repair path and the live charge ledger wait on a fresh run. Both are measured now. | `t8.1-rerun.md` claim 1 measured `atempo` at 0.9813 and 0.9844. `charge-wiring-live.md` measured 56 rows summing to 65,330,550. My run measured `atempo` ratios 1.0316, 1.0754, 0.9555 and 0.9894 and 56 rows summing to 53,571,300. | A reader believes the core repair path and the ledger are unproven. |
| `deploy/` | No script creates the ClickHouse Cloud database or loads `sql/schema.sql`. `deploy/README.md:73-74` names it as a manual prerequisite. | `grep -rn schema.sql deploy/ systemd/ Caddyfile` returns only that README line. The scripts rebuild the VM, disk, bucket, firewall, service account, secrets, mcp image, units and Caddy. | A lost ledger backend is not rebuildable from the repository alone. |
| `dev-diary/adversarial-review/t8.4-submission.md:133` | The record says the T8.4 status line stays `not-started`. The phase document reads `claimed:t84submission`. | `dev-diary/PHASE-8-ship.md:99`. | A reader trusts a stale status note. |
| `web/src/lib/tabs/DetailsTab.svelte:92` | The per line charge list labels two rows `Translation for try 3` with no unit, so a prompt row and a candidate row look identical. | The live tab lists `Translation for try 3 $0.000006` and `Translation for try 3 $0.00001305`. | The breakdown is ambiguous. |

## Exit criteria

### 1. The sample runs end to end against live services

**PASS.** I drove dub `b82693911467eae901626876e4a045a2` from `POST /api/dubs/sample` to the
terminal event. Run id `9b48f9f5c65258b15bd2163000e99ece`, run POST `2026-09-09T17:10:59Z`, stream
close `2026-09-09T17:15:19Z`, 260 seconds. The terminal event read
`{"type":"done","sentence":"Dubbing pipeline completed with 3 flagged lines.","total_nanodollars":53571300}`.
The stream delivered frames while the run proceeded, so Caddy does not buffer. The export measures
75.008267 seconds and 18,380,363 bytes with h264 video and aac audio at 44100 Hz and 2 channels.
The ledger holds 56 charge rows with 56 distinct `event_key` values summing to 53,571,300
nanodollars, which equals the run total exactly.

### 2. Every measured baseline from validation reproduces or improves

**PASS on behaviour.** No baseline row reproduces numerically, because the live model output
differs from the 2026-09-07 run. The table compares `observations.md` Section 1 against my run.

| seg | baseline slot / take / delta / repair / outcome | live slot / take / delta / repair / outcome | verdict |
|---:|---|---|---|
| 1 | 1820 / 1680 / -7.7% / none / short | 1900 / 1911 / +0.6% / atempo 1.0316 / fits | differs, improves |
| 2 | 5480 / 5320 / -2.9% / none / fits | 4270 / 4264 / -0.1% / atempo 0.9555 / fits | differs, holds |
| 3 | 5320 / 5720 / +7.5% / atempo / repaired | 5870 / 6520 / +11.1% / rewrite / flagged too_long | differs, the overrun exceeds the budget |
| 4 | 5660 / 5920 / +4.6% / atempo / repaired | 5360 / 5400 / +0.7% / none / fits | differs, holds |
| 5 | 4840 / 4720 / -2.5% / none / fits | 5450 / 6440 / +18.2% / rewrite / flagged too_long | differs |
| 6 | 4630 / 4440 / -4.1% / none / short | 5170 / 4440 / -14.1% / rewrite / flagged too_short | differs, improves |
| 7 | 7040 / 6800 / -3.4% / none / fits | 7960 / 7966 / +0.1% / atempo 1.0754 / fits | differs, holds |
| 8 | 7110 / 4200 / -40.9% / none / broken, reported as fit | 8530 / 8523 / -0.1% / atempo 0.9894 / fits | differs, improves |

The behaviour claims hold:

- Time stretch repair reproduces and improves. Four segments stretched, in both directions.
  Ratios above 1 speed up an overrun and ratios below 1 slow an underrun.
- A short take reports as a miss. Line 6 reads `Line 6 length bar. This take is 0.73 seconds short
  of the slot.`
- The tail carries audio. The export last five seconds measure mean -16.7 dB and max -0.5 dB
  against the source mean -15.1 dB and max -0.1 dB. Digital silence measures -91 dB. The speech
  free gap 53.2 to 57.5 seconds measures source -16.1 dB and export -16.1 dB.
- Speech sits at full level. Least squares gives `alpha_speech` 0.9874 and `g_bed` 0.3259 over
  segment 1, and `alpha_speech` 0.9853 and `g_bed` 0.3208 over segment 8. The gap gives
  `alpha_speech` 0.0000 and `g_bed` 0.9993. The bed dips 6.2 dB under speech.
- The charges count each call once. 56 rows, 56 distinct keys, exact sum.
- The clean export defect has no product counterpart. The run writes only `dubbed_ducked.mp4`, so
  the -91 dB silence cannot recur.

The caveat is finding H1. The per line number the demo shows does not match the ledger.

### 3. A clean instance rebuilds from the repository alone

**MET FOR THE VM, NOT FOR THE LEDGER BACKEND.** `deploy/` holds a complete and idempotent path
for the machine. `provision.sh` creates the address, instance, disk, bucket, firewall, service
account and five secrets. `build.sh` stages the binary, the web build, testdata, deploy, systemd
and the Caddyfile. `ship.sh` swaps `/opt/ajilamu` atomically. `bootstrap.sh` installs packages,
mounts the disk, builds the mcp image, installs the units and Caddy and starts every service.
`verify.sh` measures over TLS. `t8.2-deploy.md` records `deploy/deploy.sh --recreate-vm` deleting
and rebuilding the instance and then serving run 3's export byte for byte at sha256
`06cf7f2081eb719c1381b857074b7ef9055949d69ac2800731e94c46459858cb`. I did not repeat that,
because the assignment forbids destroying the host. No script creates the ClickHouse Cloud service
or loads `sql/schema.sql`. `deploy/README.md` names that as a manual prerequisite. Finding M3
covers the gap.

### 4. The README names what remains unbuilt

**PARTIAL.** The section `What this does not do yet` names two true limits. On-screen text is not
extracted. Speaker identity comes from voice rather than from the picture. Its third bullet is
false now, because the live repair path and the charge ledger are measured. The README also omits
the clean export gap that `docs/submission.md` names. Findings M1 and M2 cover the false
statements.

### 5. Submitted

**NOT MET.** The T8.4 status reads `claimed:t84submission`, and its done condition is a submitted
entry. The phase handoff log states the remaining actions belong to the user, namely record the
video, upload it, paste the entry and submit. The artifacts exist and the click path works, so the
task is ready to submit and is not submitted. The phase cannot close.

## Per task residue

### T8.1 end to end on the sample

Closed on the deployed host. I re measured all five claims independently on a new live run. Claim 1
passes with four `atempo` repairs. Claim 2 passes with line 6 named short. Claim 3 passes at -16.7
dB. Claim 4 passes at `alpha_speech` 0.9874. Claim 5 passes with 56 rows and an exact sum.
Residue is H1, which makes the per line cost on the Details tab wrong. The stale `default`
database still carries the validation shape, and the `ajilamu` database serves every read and
write.

### T8.2 deploy

The host serves revision `9fae43f451d3e4cc7d7fc337498a94a05710be95`. `git diff --name-only 9fae43f
HEAD` touches only diary and docs, so the host runs the current application code. All four units
report active. `ffmpeg` reports 6.1.1-3ubuntu5, which matches the recorded drift from the frozen
n9.0.1. Both hostnames answer 200 and a range request answers 206 with `content-range: bytes
0-1023/18380363`. Residue is M3 and the recorded ffmpeg drift.

### T8.3 documentation

The live documentation site answers 200. The round one review approved the build. Residue is M1
and M2, because the site repeats the 8 percent short budget and the stale unverified edge cases.

### T8.4 submission

The artifacts exist and both shots work. The shot 1 reading is correct. The shot 2 line total is
wrong, which is H2. The task is not submitted. The record's status note is stale, which is L1.

## Additional checks

- **Index readiness read: PASS.** Every live index row matches the ledger. The reconciled dubs
  read `review` at 65,330,550, 56,884,350, 72,309,000 and 67,315,200. The failed wiring dub reads
  `review` at 0. The two uploads with no ledger rows read `pending` at 0. The fixture row
  `d3bca364` reads `review` at 23,414,000 from the committed manifest rather than from the ledger.
- **Per attempt charge identity: PASS.** `38fa4e9c` stores identical calls under distinct
  `event_key` values. Translate prompt rows of 105 units at 0.00000015 USD store two rows on
  attempts 1 and 2.
- **Production charge wiring: PASS.** My run wrote 56 charge rows through the deployed binary, and
  the sum equals the run total.
- **Wordmark: PASS.** The chrome serves `/brand/original-light.svg` at natural width 589.
- **Docs link: PASS.** The chrome links `https://nrynss.github.io/ajilamu/`, which answers 200.
- **Ignored agent directories: PASS.** `.gitignore:28-36` ignores nine tooling directories,
  `git ls-files` tracks none of them, and `git check-ignore` matches.
- **Repository drift audit: PASS.** `python3 tools/audit_docs.py` exits 0.
- **In flight readiness: noted.** A running project reads `Queued to start` on the index until its
  takes land, because the index cannot see the run registry. `index-readiness.md` names this and
  no task owns it.
- **Source URL: noted.** `GET /api/dubs/{id}/source.mp4` answers 404. The sample copies the
  committed `testdata/clip.mp4`, so no record promised that route.

## Evidence paths

All under `/tmp/p8close-live` unless stated.

| Path | Contents |
|---|---|
| `sample.json`, `run.json`, `run-post-utc.txt`, `events-close-utc.txt` | the sample id, run id and timestamps |
| `events.sse` | the full event stream, 219 lines and the terminal `done` |
| `workspace-38fa.json`, `workspace-b826.json` | the two workspace payloads |
| `b826-dubbed_ducked.mp4`, `b826-bed.wav`, `b826-speech.wav` | the export and the two assembly layers |
| `b826-source.mp4` | the 404 body for the source route |
| `chq.sh` | the ClickHouse query helper, no credential in the file |
| `dubbed_ducked.mp4`, `bed.wav`, `speech.wav` | the copies the fit script reads |
| terminal output | the ledger sums, the take table, the volumedetect rows and the least squares fit |

## Cleanup

I made no copy of the repository, so there is none to delete. I started no container, and
`docker ps` reports none. I closed every browser tab. The live host still runs and serves both
names. The only change I made to it is the sample run the assignment required. The main checkout
carries only this untracked record.
