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
owns:       (integration, no exclusive paths)
status:     not-started
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
status:     not-started
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
owns:       README.md, docs/
status:     not-started
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
owns:       docs/submission.md
status:     not-started
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
