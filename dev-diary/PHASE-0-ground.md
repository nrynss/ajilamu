# P0: Ground

```yaml
id:       P0
size:     S
requires: []
blocks:   everything
parallel: no
```

**Goal:** Establish clean git version control, remove secrets from source code, implement the media wrapper in Go, and promote test fixtures.

**Why this phase runs serially:** Four simple tasks share foundational setup. Phase P1 cannot define the `Take` struct without verified `ffprobe` output. Downstream tasks require committed fixtures in `testdata/`.

---

### T0.1: Repository on git footing ★
```yaml
requires:   []
fixture-ok: no
size:       XS · light
owns:       .gitignore, tools/validate_pipeline.py
status:     done
```
The repository currently lacks commits.

Move `scratch/validate_pipeline.py` to `tools/validate_pipeline.py` and track it with git. Downstream tasks port logic from this reference script.

Keep `scratch/` ignored. Ignore `.tgz` archives and large source videos in `assets/source/*.mp4`. Do not ignore `testdata/`.

Commit the documentation files, `.env.example`, and the `tools` directory.

**Done when:** `git log` shows initial commits, a fresh clone contains documentation and reference scripts, and `git status` is clean.

---

### T0.2: Credentials out of source ★
```yaml
requires:   []
fixture-ok: no
size:       S · mid
owns:       internal/config/config.go, .env.example
status:     done
```
The initial test script contained a fallback ClickHouse password.

Rotate this database password first. Remove all fallback credentials from scripts and documentation.

Build a Go configuration loader that separates process settings from credentials. Process settings load at startup with sensible defaults.

Development and fixture mode defer credentials until the feature that needs them. A missing credential fails that request with an error naming its environment variable.

`ENV=production` validates ClickHouse and Google Cloud settings at boot. A deployed host never starts in degraded fixture mode.

Non-secrets like `PORT`, `ENV`, and `GEMINI_MODEL` may define sensible defaults. `AJILAMU_DATA_DIR` names the application data directory. `AJILAMU_FRONTEND_DIR` names the frontend build root. The Go client must consume these values directly.

**Done when:** The old password is dead, `git grep` finds zero credential literals, fixture mode starts with no credentials, and production fails on missing credentials.

---

### T0.3: Media wrapper spike ★
```yaml
requires:   T0.1
fixture-ok: no
size:       M · mid
owns:       go.mod, go.sum, internal/media/probe.go, internal/media/ffmpeg.go
status:     done
```
Initialize the Go module. Implement five core media operations wrapping ffmpeg:

* `Duration(path) (time.Duration, error)`: Runs `ffprobe` to query duration. Always return structured time types, not raw floats.
* `AudioFormat(path) (Format, error)`: Queries sample rate, channel count, layout, and codec. Downstream assembly uses this data to match source audio quality.
* `Demux(video, wav) error`: Extracts 16 kHz mono audio for Gemini analysis only. This temporary audio never enters final mixes.
* `Atempo(in, out string, ratio float64) error`: Must accept ratios below 1.0 to slow down short takes. Reject ratios outside the 0.5 to 2.0 range.
* `Run(args...)`: Executes ffmpeg commands, captures standard error, and returns informative errors.

Test these functions against files in `assets/source/clip.mp4` and `scratch/takes/`.

**Done when:** Go tests verify `seg_3_try1.wav` measures 5720 ms and `seg_3_stretched.wav` measures 5338 ms. Tests must also slow down `seg_8_try1.wav` successfully.

---

### T0.4: Promote artifacts to fixtures ★
```yaml
requires:   T0.1
fixture-ok: no
size:       S · mid
owns:       testdata/
status:     done
```
The validation run produced real audio assets. Commit them to `testdata/` to unblock parallel tracks.

Copy these files into `testdata/`:
* `testdata/segments.json`: Eight dialogue segments with timestamps and speakers.
* `testdata/takes/`: Ten WAV files, including time-stretched takes.
* `testdata/clip.mp4`: The 75-second source clip for audio bed testing.
* `testdata/expected/`: Golden JSON metrics containing target slots, actual durations, signed deltas, and applied repairs.

**Done when:** `testdata/` is committed to git, and unit tests verify duration measurements against golden expectations.

---

## Exit Criteria

- [x] Repository contains initial commits and tracks `tools/validate_pipeline.py`.
- [x] Rotated credentials and removed hardcoded secrets from source code.
- [x] `internal/media` measures duration and stretches audio in both directions.
- [x] Committed `testdata/` fixtures to git.

---

## Handoff Log

### What exists now
All four foundational tasks in Phase 0 are complete.
We established git version control with a private GitHub repository.
We rotated the ClickHouse Cloud database password and verified zero hardcoded credentials remain.
The Go module `github.com/nrynss/ajilamu` builds under Go 1.27.1.
Package `internal/config` loads required secrets and sets default values.
Package `internal/media` measures audio duration, inspects format, demuxes mono audio, and executes bidirectional time stretching via `atempo`.
Committed test fixtures in `testdata/` provide the reference 75-second clip, 8 segments, 10 WAV takes, golden metrics, and automated duration tests.

### What surprised us
ClickHouse Cloud enforces password complexity requirements, requiring at least one special character.
The ClickHouse Cloud OpenAPI accepts a PATCH request on the password endpoint to rotate credentials securely.
Go ignores directories named `testdata` when using `...` package wildcards.
Targeting `./testdata` directly runs the fixture test suite as expected.

### Notes for the next developer
Phase P1 (Contracts and fixtures) is now unblocked.
You can run all audio and ledger tests offline against `testdata/` without live API keys.
When implementing P1 data contracts, bind `Take` structs to the measured millisecond durations and golden metrics in `testdata/expected/metrics.json`.
