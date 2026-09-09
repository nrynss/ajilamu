# Ajilamu

**Give a film another tongue, and keep its rhythm.**

Ajilamu dubs creator video into another language and holds every line inside the slot the
original speaker left for it. It measures each synthesized take against its slot, repairs the
misfits, and records what every call cost.

It is a Go server, a Svelte 5 workspace, and an append-only ledger in ClickHouse.

Online documentation lives at [nrynss.github.io/ajilamu](https://nrynss.github.io/ajilamu/).

---

## Where the name comes from

Aji Lhamu is the masked dance-drama of the Monpa people in western Arunachal Pradesh. It is
about six centuries old. Actors perform it across five days in dance, song, and pantomime.

The play carries the Tibetan telling of the Ramayana. That story had already crossed several
languages and borders before it reached the Monpa valleys. The performers carry it across one
more.

The tradition descends from Thangtong Gyalpo, the fifteenth-century Tibetan engineer and
dramatist. He staged performances to raise money for iron suspension bridges across Himalayan
gorges. The Mukto Monpa tradition credits the dance with financing 108 of them.

Two things in that history name this project.

A masked player cannot rely on a face. The mask fixes the expression, so the voice has to land
on the movement exactly. That is the dubbing problem, six centuries early.

And the theatre paid for bridges. A performance that carries a story between languages funded
the crossings that carried people. Software that moves a film between languages sits in the
same line of work.

[`dev-diary/project.md`](dev-diary/project.md) holds the fuller note on the name.

---

## What the system does

You give it a video and a target language. It returns a dubbed video and the audit trail behind
it.

1. **Watch.** One Gemini 3.8 Flash pass over the source returns timestamped segments with speaker names
   and emotional register.
2. **Translate under a budget.** Each line goes to Gemini with its slot length as a hard
   constraint, not as a hint.
3. **Render.** Google Cloud Chirp 3 HD synthesizes the take. The voice follows the documented
   gender of the assigned speaker.
4. **Measure.** `ffprobe` reads the take. The system stores a signed delta against the slot,
   so a short line and a long line both register.
5. **Repair.** See the next section.
6. **Assemble.** ffmpeg lays the takes over the original audio bed and muxes the result.

The original background audio survives everywhere outside the dialogue slots. Music, room tone
and effects between lines belong to the film, and the export keeps them. The export adopts the
source sample rate, channel count and layout, so the film never gets downsampled into the
takes.

The 16 kHz mono demux serves analysis only. It never reaches the final mix.

Every take stays a discrete WAV file on disk until export. That is what makes line soloing,
prior-take preview and non-destructive retries possible.

---

## Fit is two-sided

A line that ends early breaks lip sync exactly as a line that overruns does. The threshold
therefore reads the absolute delta, and the direction picks the repair.

| Signed delta | What happens |
|---|---|
| Within 8 percent long or 5 percent short | ffmpeg `atempo` speeds up an overrun or slows an underrun. No model call, no fee. |
| More than 8 percent long | Gemini rewrites the line shorter. |
| More than 5 percent short | Gemini rewrites the line fuller. |
| Still wrong after 3 attempts | The line is flagged for creator review, and the workspace says so. |

The short side carries the tighter budget, because slowing speech is more noticeable than
speeding it up. The threshold lives at `types.FitThreshold`. The stretch budgets live at
`fit.DefaultMaxStretchLong` and `fit.DefaultMaxStretchShort`. The attempt cap lives at
`fit.DefaultMaxAttempts`.

This rule exists because the first validation run got it wrong. It reported a take running 40.9
percent short of its slot as a clean fit, and left 2.9 seconds of dead air over a moving mouth.
[`dev-diary/observations.md`](dev-diary/observations.md) records that run and its measurements.

---

## What the ledger records

Every take, edit and command appends a row to ClickHouse. Nothing overwrites state.
[`sql/schema.sql`](sql/schema.sql) is the whole definition, and running it twice is safe.

| Table | Holds |
|---|---|
| `takes_raw` | Slot duration, measured duration, signed delta, repair strategy, take path, waveform peaks |
| `commits_raw` | The edit DAG: commit id, parent commit id, version sequence |
| `timeline_state_raw` | Segment boundaries, speaker and text at each commit |
| `actions_raw` | What happened, who did it (`agent`, `command_bar`, `manual_ui`), and the original command text |
| `charges_raw` | One row per billed API call, itemized |

Views of the same names read the deduplicated truth. `timeline_at_commit` reconstructs the
timeline at any point in its history, which is how time travel and A/B branch comparison work.

Waveform peaks live in the ledger as `Array(UInt8)`, so the workspace draws a waveform without
reading the audio file.

Writes go through the durable queue in `internal/ledger`, which is the single writer. The editor
agent reads through a self-hosted mcp-clickhouse server and can never insert. If that server
fails, the agent loses its read tools and nothing else stops.

---

## What a dub costs

The rate card sits in [`internal/cost/cost.go`](internal/cost/cost.go) and prices in
nanodollars, so a real fee never rounds to zero on screen.

| Call | Rate |
|---|---|
| Chirp 3 HD synthesis | $30.00 per million characters |
| Gemini 3.8 Flash prompt tokens | $0.15 per million |
| Gemini 3.8 Flash output tokens | $0.60 per million |

The measured reference is the 75-second NASA sample into one language.

| Sample | Fee |
|---|---|
| 75.0 s, one target language | $0.023414 |

The upload page projects a fee from that measured rate before you commit to a run. The completed
ledger is the real number. Every segmentation, translation, synthesis and agent call counts
once, and rejected attempts carry their own charges rather than hiding inside the winning take.

---

## Run it locally

### You need

- Go 1.27.1
- Node 26 and npm
- ffmpeg and ffprobe, n9.0.1 or later, with `atempo`, `amix`, `aresample` and `sidechaincompress`
- Google Cloud Application Default Credentials, for a live dub
- A ClickHouse service holding `sql/schema.sql`, for the ledger

### Configure

```bash
cp .env.example .env
```

Fill `GOOGLE_CLOUD_PROJECT` and the ClickHouse block. Authenticate with
`gcloud auth application-default login`. Do not set a Gemini API key, because the SDK
authenticates through ADC.

Required variables carry no default. A missing one stops startup with an error naming the
variable. `.env` stays gitignored, and no secret belongs in the repository.

### Build and start

```bash
npm --prefix web ci && npm --prefix web run build
go build -o dist/ajilamu ./cmd/ajilamu
./dist/ajilamu
```

The server listens on `PORT`, which defaults to 8080. Open `http://localhost:8080`.

### Complete a dub

1. Open **Create a dub** at `/new`.
2. Press **Try sample video** to copy the committed 75-second NASA clip into a fresh project.
   Alternatively drop in your own video, and an optional separate music track.
3. Pick the target language. The sample runs into Malayalam.
4. The workspace opens at `/d/<id>` and streams live progress while the run works.
5. Correct anything the model got wrong. Drag a boundary, reassign a speaker, fix the text, or
   type an instruction into the command bar.
6. Play the export.

Every correction becomes a commit, so the history tab shows what changed and why.

### Run it with no cloud at all

The server starts without credentials. It loads the committed fixture workspace, so the
timeline, the length bar, the history tab and the ledger views all render. Dubbing runs stay
unavailable, and the log says so at startup. This is the offline path for interface work.

### Tests

```bash
go test ./...
npm --prefix web run check
python3 tools/audit_docs.py
```

The last one checks the documents against `go.mod`, `.env.example` and the filesystem. Exit code
1 means the docs and the repository disagree.

---

## Deploy

[`deploy/README.md`](deploy/README.md) rebuilds the whole host from an empty Google Cloud
project. It provisions the machine, the disk, the bucket, the service account and every secret,
builds the release bundle, ships it, and then measures the result from outside.

Caddy terminates TLS and is the only public listener. The Go server and the mcp-clickhouse
container both bind loopback. Secrets reach the process through Secret Manager and a tmpfs file
at `/run/ajilamu/env`. No `.env` file ever reaches the host.

---

## What this does not do yet

We would rather you read this than find it yourself.

- **On-screen text is not extracted.** The segmentation prompt asks for speech only. Signage,
  captions and titles in the frame pass through untouched. The stack can carry video frames, and
  the schema has no field for the result.
- **Speaker identity is inferred from voice, not from the picture.** The model names speakers
  from how they sound. It has misspelled a name across segments of one clip, and the synthesizer
  read the misspelling aloud. The speaker selector on the timeline exists because of this.
- **The deployed host runs ffmpeg 6.1.1-3ubuntu5.** `AGENTS.md` freezes n9.0.1. The filters
  used, `atempo`, `amix`, `aresample` and `sidechaincompress`, behave the same in both builds,
  but the versions differ.
- **Ducking against a supplied music track is the clean path.** Dynamic ducking against the film
  mix is the fallback when no separate music track exists.

---

## Repository map

| Path | Holds |
|---|---|
| `cmd/ajilamu` | The server entry point, and the adapters that wire the packages together |
| `internal/fit` | The generate, measure, repair, retry loop |
| `internal/gemini` | Segmentation and duration-constrained translation |
| `internal/tts` | Chirp 3 HD synthesis, the voice assignment and the language catalog |
| `internal/assemble` | Take placement, the background bed, ducking and the mux |
| `internal/ledger` | The durable ClickHouse writer and the read views |
| `internal/agent` | The editor agent on ADK, reading the ledger over MCP |
| `internal/command` | Natural language command parsing into deterministic mutations |
| `internal/api` | HTTP routes, progress streaming and static serving |
| `web` | The Svelte 5 workspace |
| `docs` | The documentation site on Astro and Starlight |
| `sql/schema.sql` | The ledger definition |
| `deploy`, `systemd`, `Caddyfile` | The host, rebuildable from scratch |
| `dev-diary` | The specification, the phase breakdown and every review round |
| `testdata` | Real takes and exports from the validation run |

---

## Working in this repository

[`AGENTS.md`](AGENTS.md) binds every human and coding agent here. Read it before you change
anything.

Two of its rules matter more than the rest.

**A pin is a measurement, never a log line.** Software reports its own success, and that report
is the thing under review rather than evidence for it. Run `ffprobe`, query the database, send a
request, screenshot the page. The validation run printed "perfect fit" for a take that ran 40.9
percent short.

**A blocked task is a finding, not a cut.** Record it, name what depends on it, and leave it on
the board.
