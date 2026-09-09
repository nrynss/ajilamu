# Ajilamu

**Give a film another tongue, and keep its rhythm.**

Ajilamu dubs creator video into another language and holds every line inside the slot the
original speaker left for it. It measures each synthesized take against its slot, repairs the
misfits, and records what every call cost.

- Live product: <https://ajilamu.nryn.dev>
- Documentation: <https://nrynss.github.io/ajilamu/>
- Source: <https://github.com/nrynss/ajilamu>

---

## What it does

You give Ajilamu a video and a target language. It returns a dubbed video and the audit trail
behind it.

1. **Watch.** One Gemini 3.8 Flash pass returns timestamped segments with speaker names and
   emotional register (`README.md`).
2. **Translate under a budget.** Each line goes to Gemini with its slot length as a hard
   constraint (`README.md`).
3. **Render.** Google Cloud Chirp 3 HD synthesizes the take (`README.md`).
4. **Measure.** `ffprobe` reads the take, and the system stores a signed delta against the slot
   (`README.md`).
5. **Repair.** The fit loop rewrites or time-stretches the line.
6. **Assemble.** ffmpeg lays the takes over the original audio bed and muxes the result
   (`README.md`).

The original music, room tone, and effects survive outside the dialogue slots (`README.md`).
The export adopts the source sample rate, channel count, and layout (`README.md`). A measured
run exported a 75.008267-second video at 18,379,625 bytes
(`dev-diary/adversarial-review/t8.2-deploy.md`, run 3).

---

## Fit is two-sided

A line that ends early breaks lip sync as badly as a line that overruns. The system measures the
absolute delta, and the direction picks the repair. The budget is direction-aware.
`fit.StretchDeadBand` is the tolerance that ships a take untouched (`internal/fit/stretch.go`).
`fit.DefaultMaxStretchLong` and `fit.DefaultMaxStretchShort` set what `atempo` may repair
(`internal/fit/stretch.go`). `fit.DefaultMaxAttempts` caps the loop at 3
(`internal/fit/rewrite.go`). `types.FitThreshold` governs the reported fit state
(`internal/types/types.go`).

| Signed delta | What happens |
|---|---|
| Inside the dead band | The take ships as it stands. |
| Beyond the band, long, inside 8 percent | ffmpeg `atempo` speeds the line up. No model call and no fee. |
| Beyond the band, short, inside 5 percent | ffmpeg `atempo` slows the line down. No model call and no fee. |
| Beyond the budget, long | Gemini rewrites the line shorter. |
| Beyond the budget, short | Gemini rewrites the line fuller. |
| Still wrong after 3 attempts | The line is flagged for creator review. |

The 2026-09-07 baseline proved why this rule exists. It reported a take running 40.9 percent
short of its slot as a clean fit. It left 2.9 seconds of dead air over a moving mouth
(`dev-diary/observations.md`, Sections 1 and 2.1).

The live product repairs in both directions. Segments 3 and 4 repaired by `atempo` at 0.9813 and
0.9844, and both takes landed inside their slots
(`dev-diary/adversarial-review/t8.1-rerun.md`, claim 1). A short take now reports as a miss, and
the length bar names the shortfall. One live run rendered "Line 7 length bar. This take is 0.64
seconds short of the slot." (`t8.1-rerun.md`, claim 2). The demo runbook reads 1.42 seconds short
on a second live run (`dev-diary/adversarial-review/t8.4-submission.md`).

---

## The provenance ledger

Every take, edit, and command appends a row to ClickHouse, and nothing overwrites state. The
tables are `takes_raw`, `commits_raw`, `timeline_state_raw`, `actions_raw`, and `charges_raw`
(`README.md`, `sql/schema.sql`). Views of the same names read the deduplicated truth, and
`timeline_at_commit` reconstructs the timeline at any point (`README.md`).

Writes go through the durable queue in `internal/ledger`, the single writer (`README.md`). The
editor agent reads through a self-hosted mcp-clickhouse server and can never insert
(`README.md`).

The ledger reconciles to the nanodollar. A live run wrote 56 rows with 56 distinct event keys
summing to 65,330,550 nanodollars, which equals the run-reported total
(`dev-diary/adversarial-review/charge-wiring-live.md`). The same run billed 1 segment call, 18
translation calls, and 18 render calls (`charge-wiring-live.md`). Rejected attempts carry their
own charges, so the workspace can show what one line cost across every try.

---

## What a dub costs

The rate card lives in `internal/cost/cost.go` and prices in nanodollars.

| Call | Rate |
|---|---|
| Chirp 3 HD synthesis | $30.00 per million characters |
| Gemini 3.8 Flash prompt tokens | $0.15 per million |
| Gemini 3.8 Flash output tokens | $0.60 per million |

The committed fixture total for the 75-second sample is $0.023414, covering 1 segmentation pass,
8 translations, and 8 voice renders (`testdata/wire/total.json`, measured in
`dev-diary/adversarial-review/t1.4-round1.md`). The upload page projects from that fixture, and
the completed ledger is the real number (`README.md`).

A live run bills more calls than the fixture, so it costs more. Measured ledger costs were
$0.05688435 (`dev-diary/adversarial-review/t8.1-rerun.md`), $0.06533055
(`charge-wiring-live.md`), $0.0673152 and $0.072309 (`t8.2-deploy.md`).

A run takes about four minutes. Measured wall times were 239.44 seconds
(`charge-wiring-live.md`), 261 seconds (`t8.1-rerun.md`), and 274 seconds (`t8.2-deploy.md`,
run 3).

The host costs about $59.42 per month at published us-central1 rates measured on 2026-09-09
(`t8.2-deploy.md`). Model and speech calls add to that.

---

## The host

The product runs on a Google Compute Engine `e2-standard-2` in us-central1
(`dev-diary/infrastructure.md`). Caddy terminates TLS and is the only public listener
(`README.md`). A clean instance rebuilt from the repository alone served an earlier export byte
for byte, sha256 `06cf7f20` (`t8.2-deploy.md`). Both hostnames answer 200, and a range request
answers 206 (`t8.2-deploy.md`).

---

## What remains unbuilt

We would rather you read this than find it yourself.

- **On-screen text is not extracted.** Signage, captions, and titles pass through untouched
  (`README.md`, `docs/src/content/docs/reference/limitations.md`).
- **Speaker identity comes from voice, not from the picture.** The model has misspelled a name
  across segments, and the synthesizer read the misspelling aloud (`README.md`,
  `limitations.md`).
- **Live segmentation may return a different line count than the 2026-09-07 baseline.** The
  baseline returned 8 segments (`dev-diary/observations.md`). The 2026-09-09 live run returned 7
  lines (`t8.1-rerun.md`), so segment 8 has no live counterpart. The behaviour the baseline hid
  still holds, because the short line reports as a miss.
- **Baseline clean-export row:** no product counterpart. That run wrote a clean export that
  measured -91.0 dB in the tail, which is digital silence, and downgraded the film to 16 kHz
  mono (`observations.md`, Section 2.2). The product writes only the ducked export when no
  separate music track exists (`t8.1-rerun.md`, defects outside T8.1). A supplied music track is
  the clean path.
- **The short-side stretch budget is unverified by ear.** `fit.DefaultMaxStretchShort` is 5
  percent, and no listener has ruled on it (`internal/fit/stretch.go`).
- **The deployed host runs ffmpeg 6.1.1-3ubuntu5**, while `AGENTS.md` freezes n9.0.1. Every
  measured run used the host build (`t8.2-deploy.md`).

---

## See it work

The demo runbook shows the two shots that make the honest-instrument argument visible
(`docs/demo-runbook.md`). A length bar catches a take that runs too short. The ledger reports
what one line cost.
