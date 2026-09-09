# P8: Ship

```yaml
id:       P8
size:     M
branch:   phase/p8-ship
requires: [P2, P3, P5, P7]
blocks:   submission
parallel: no
```

**Goal:** Run the complete system on one machine, prove it by measurement, and describe it
honestly.

**Serial by decision.** These tasks share the server entry point and the deploy surface.
Parallel agents would collide in the one file the demo depends on.

---

### T8.1: End to end on the sample ★
```yaml
requires:   P2, P3, P5, P7
fixture-ok: no
size:       M · frontier
owns:       (integration only)
status:     done
```
Drive the sample button through live services to a finished video. Keep the workspace
responsive for the whole run.

Check the result against the measured baseline in [observations.md](observations.md) Section 1.
That table exists for this task.

Measure each claim independently. Reading the run log proves nothing, because the validation
script reported every one of its defects as a success.

* Segments 3 and 4 repair by time-stretch, matching validation.
* Segment 8 reports as a miss rather than a fit.
* The exported tail carries audio, not silence.
* The ducked export plays speech at full level.
* Logged charges cover every Gemini call and count each one once.

**Done when:** Each line above passes an independent measurement recorded in the review file.

---

### T8.2: Deploy ★
```yaml
requires:   T8.1
fixture-ok: no
size:       M · mid
owns:       deploy/, Caddyfile, systemd/
status:     done
```
Provision the machine described in [infrastructure.md](infrastructure.md). Build the
mcp-clickhouse image from `deploy/mcp-clickhouse/`, because no published image exists.
Run the Go server, that container for agent reads, Caddy as reverse proxy, a 50 GB SSD at
`/data/storage`, and the archive bucket.

Provision configuration the way "Configuration and Secrets" in that document describes. Create
each Secret Manager secret, grant `roles/secretmanager.secretAccessor` on the individual secret
rather than the project, and put the non-secret settings in the systemd unit. The `ExecStartPre`
step fetches secrets to `/run/ajilamu/env` on tmpfs at mode `0600`. No `.env` file ships to the
host, and `GOOGLE_APPLICATION_CREDENTIALS` stays unset so ADC reads the metadata server.

Serve video over HTTP range requests so scrubbing stays instant.

Keep the box reproducible from the repository. If the instance dies, the scripts rebuild it.
Nobody reconstructs it from shell history.

**Done when:** A clean instance built from `deploy/` alone serves a finished dub over TLS.

---

### T8.3: Documentation
```yaml
requires:   T8.1
fixture-ok: no
size:       M · mid
owns:       README.md, docs/, .github/workflows/deploy-docs.yml
status:     done
```
Explain what Ajilamu does, how to run it, how the fit loop repairs in both directions, what the
ledger records, and what a dub costs.

Name what the system does not do. On-screen text extraction and visual speaker identity remain
unbuilt. A reader who finds a gap between the writeup and the code stops trusting both.

**Done when:** A stranger clones the repository and completes a dub from the README alone.

---

### T8.4: Submission
```yaml
requires:   T8.1, T8.2, T8.3
fixture-ok: no
size:       S · mid
owns:       docs/submission.md, docs/demo-runbook.md
status:     claimed:t84submission
```
Record the demo video and complete the entry.

Show the two things no other tool shows. A length bar catching a take that runs too short, and
a ledger reporting what one line cost. Both make the honest-instrument argument visible, and
the validation run could demonstrate neither.

**Done when:** Submitted.

---

## Exit Criteria

- [ ] The sample runs end to end against live services.
- [ ] Every measured baseline from validation reproduces or improves.
- [ ] A clean instance rebuilds from the repository alone.
- [ ] The README names what remains unbuilt.
- [ ] Submitted.

---

## Handoff Log

_(Fill on completion: record the end-to-end measurements and the deploy verification.)_

### T8.1 live run 2026-09-09

The first live run reached the export but ended in an error. Three findings came from it.

**The C is provisioned.** The live ClickHouse Cloud `default` database carried the pre-T1.3
validation shape, so `charges_raw` had no `turn_id` or `call_index` and its kind enum lacked
`agent`. Every charge insert returned code 16. The schema guard refuses that shape by design.
A fresh database `ajilamu` now holds `sql/schema.sql`, and `.env` points at it.

**The two H findings are fixed.** The readiness probe now separates name resolution from its
dial budget, so a healthy ledger on a slow resolver reports ready. A failed run now logs its
cause with the run id, dub id and stage at the failure, while the browser sentence stays plain.

**Two of the five claims do not hold yet.** The charges claim failed only because of the stale
schema, and the fix above unblocks it. The repair claim failed because this run's live model
output put every attempt outside the stretch budget, so `atempo` never ran. T8.1 stays open
until a live re-run measures both. T8.2 proceeds at the user's direction, because the deploy
proof re-runs the same pipeline from a clean instance.

### T8.3 documentation site 2026-09-09

Astro and Starlight documentation site built in `docs/` and deployed to GitHub Pages.
Adversarial review round one approved with zero residue across all severities.

The site enforces a three-column layout across all pages via `template: doc`.
A custom theme applies obsidian dark styling with Monpa copper and cyan telemetry accents.
GitHub Actions workflow `.github/workflows/deploy-docs.yml` publishes to GitHub Pages on master pushes.
The repository drift audit and all prose constraints pass with zero findings.

### T8.2 deploy 2026-09-09

The host is live. `https://ajilamu.nryn.dev` serves the workspace with a Let's Encrypt
certificate, and `34-63-219-30.sslip.io` stays as the fallback.

**Resources.** VM `ajilamu`, e2-standard-2 in us-central1-a, static IP 34.63.219.30, a 20 GB
boot disk and a 50 GB `pd-ssd` data disk at `/data/storage`, firewall rules for tcp 80 and 443,
the bucket `gs://ajilamu-media`, the service account `ajilamu-host@nryn-personal`, and five
Secret Manager secrets. ClickHouse Cloud holds the `mcp_readonly` user with SELECT on `ajilamu`.

**Proof.** `deploy/deploy.sh --recreate-vm` deleted the instance and rebuilt it from the
repository alone with the same address and data disk. It then served run 3's export byte
identical, sha256 `06cf7f20`. Both hostnames answer 200 and a range request answers 206. The
ledger held 62 charges with 62 distinct event keys summing to the run's reported 72,309,000
nanodollars. The events stream delivered its first frame at 0 seconds and 72 frames before
finishing, so Caddy does not buffer.

**Findings fixed inside the task.** No predefined role carries any `texttospeech.*` permission,
and Chirp synthesizes with `roles/aiplatform.user` alone. The mcp unit lacked the non-secret
ClickHouse settings and dialled localhost. Three smaller script defects are recorded.

**Cost.** About $59.42 per month fixed, and about $0.07 of model spend per dub.

### T8.1 closed 2026-09-09

The deployed host carries the proof. Five claims were measured on a live run at
`https://ajilamu.nryn.dev`.

Claim 1 passes. Segments 3 and 4 repaired by `atempo` at 0.9813 and 0.9844 and landed inside
their slots. Claim 3 passes. The tail carries audio, measured at -16.9 dB mean on the export
against -15.4 dB on the source. Claim 4 passes. Speech sits at 0.9968 of unity while the bed
dips to 0.5078. Claim 5 passes. The charges view holds 56 rows with 56 distinct event keys
summing to the run-reported 65,330,550 nanodollars.

Claim 2 is partial. Live segmentation returns seven lines, not the baseline's eight, so no
segment 8 exists. The behaviour the claim protects holds. The short line reports as a miss and
the length bar names the shortfall, `Line 7 length bar. This take is 0.64 seconds short of the
slot.`

The baseline does not reproduce line for line, because the live model output differs from the
2026-09-07 run. The task closes on the behaviour. `t8.1-rerun.md` and `charge-wiring-live.md`
carry the measurements.

### T8.4 artifacts ready 2026-09-09

`docs/submission.md` and `docs/demo-runbook.md` are written. Every number names the record that
measured it. The runbook drives the live host through the two shots the task names, and the
click path was verified in a headless browser.

The task stays open because its done condition is a submitted entry. The remaining actions are
the user's: record the video, upload it, paste the entry, and submit before the deadline.
