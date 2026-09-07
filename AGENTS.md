# Ajilamu: agent protocol

Binding for every human and coding agent working in this repository. It governs how work runs.
The product specification and the work breakdown live in [`dev-diary/`](dev-diary/). This file
does not repeat them.

Read this file in full before you change anything.

## The loop

Every task runs through the same cycle. Implement, review, remediate, re-review. Repeat until a
round returns APPROVE with zero findings across all severities. No task skips a round.

1. **Implement.** Edit only the paths listed in your task's `owns` line. Stay inside that seam.
2. **Review.** A reviewer reads the diff against the specification and writes
   `dev-diary/adversarial-review/t<N>-round<K>.md`. It carries a verdict of REMEDIATE or
   APPROVE, findings counted by severity, and four columns per finding. Where, what, pin, and
   mutation. Pin names the failing check. Mutation says what breaks if someone reverts the fix.
3. **Remediate.** A remediator fixes every finding and writes
   `dev-diary/adversarial-review/t<N>-remediation-round<K>.md`, one row per finding.
4. **Re-review.** Run the same review against the new commit. Loop until APPROVE, with an
   explicit claim of zero residue against every prior round.
5. **Land.** The orchestrator commits the approved task.

**Severities.** C breaks the demo. H is a real defect the demo survives. M is a real defect with
a workaround. L is polish.

**Every severity gets fixed.** "It is only an L" does not close a finding. A reviewer may record
a finding as a false positive. That judgement decides whether the defect is real, never whether
a real defect deserves a fix.

## Four roles, kept separate

| Role | Does | Never does |
|---|---|---|
| **Orchestrator** | Picks the task. Dispatches the other three. Gates the loop and refuses to advance on a dirty verdict. Lands the commit. | Implement a task. Write a verdict. Decide a finding is not worth fixing. |
| **Implementation** | Builds the task inside its `owns` paths. Raises a contract change in the review file instead of reaching outside. | Review its own work. Mark the task done. |
| **Review** | Reads the diff against the specification. Writes the verdict, the severity counts, and the four columns. | Fix anything it found. Soften a finding because the fix looks expensive. |
| **Remediation** | Fixes every finding. Writes one row per finding. | Change the verdict. Fix things nobody found, which arrives unreviewed. |

**Use a fresh agent per role per round.** The round-two reviewer must differ from the round-one
reviewer. Neither may have implemented the task.

Two failures follow from reusing an agent. An agent that reviews its own work stops being
adversarial. An agent that both finds and fixes a defect narrows the finding until its existing
fix looks sufficient.

### The orchestrator's one exemption

The orchestrator may fix an L finding directly, in the same commit, with no remediation round.
All four conditions must hold. The finding is an L. The fix cannot change behaviour. The review
file records it by name and file. Nothing about it is arguable.

A comment that misdescribes behaviour is not an exempt doc defect. It carries the severity of
the behaviour it misdescribes, because the next agent codes against it.

## A pin is a measurement, never a log line

This rule matters more here than anywhere else in the protocol.

Software reports its own success. That report is the thing under review, not evidence for it.

The 2026-09-07 validation run proves the point. Its log printed "perfect fit" for a take that
ran 40.9% short of its slot. It printed `fix_type: none` for the same take. The field notes then
copied that into "6 segments fit with room to spare".

Every defect later found came from re-measuring the artifacts with outside tools. None of them
appeared in the log.

So a pin observes the artifact independently. Run `ffprobe` or `volumedetect` on the audio.
Query the database. Send a request to the running service. Screenshot the page. A reviewer who
quotes program output has not reviewed.

## Task shape and dispatch

Tasks live in `dev-diary/PHASE-*.md`. Each one carries this block.

```yaml
requires:   T1.1, T3.1
fixture-ok: yes
size:       M · frontier
owns:       internal/assemble/place.go
status:     not-started
```

- **requires** binds. Task ids only, and they may cross phases.
- **fixture-ok** says whether the task can start against `testdata/` before its real upstream
  exists.
- **size** estimates the work, from XS to XL. **class** says which agent to send.
- **owns** lists the paths this task writes. No two concurrent tasks may own the same path.
- **status** reads `not-started`, `claimed:<agent>`, or `done`. Claim a task by editing the line
  before you start.

**Phase-level requires is advisory. Task-level requires is binding.** Phases exist for reading.
Tasks exist for scheduling. Start when your own named upstreams land, not when a phase closes.

### Agent class

Size and class disagree on purpose. A tiny task can still need the strongest model available.
One shared type file that six tracks compile against outweighs a thousand lines of screen
layout written against an exact specification.

| Class | Send it for |
|---|---|
| **frontier** | A decision others build on. Fiddly work verified by measurement. A specification that needs interpreting rather than transcribing. |
| **mid** | Well specified implementation. A reference to port, or a specification precise enough to follow literally. |
| **light** | Mechanical, low ambiguity work that the test shipping with it verifies. |

### Live work splits into a b-track

Work that needs credentials, an operator, or the outside world does not belong in the track that
writes the code. Split it into a `b` suffixed sibling, following the T1 and T1b pattern.

A b-track owns only the live test files in each package its parent owns. Live probes sit behind
a build tag. They never run in CI and never gate a build.

A live call is evidence, never a gate. It needs a real key, it costs real money, and another
party's outage must not redden our build. Commit the probe rather than a transcript, because
nobody can re-run a pasted console log. Paste the transcript into the record as well, since the
response shape usually matters more than the pass or fail.

## Scope

Every task in `dev-diary/` ships. Nothing carries optional status.

A task that turns out genuinely blocked becomes a finding, not a cut. Record it in the handoff
log, name every task that depends on it, and leave it on the board marked blocked. Never
reclassify it as something the project did not need.

A task too large to finish gets split. Add `T4.2a` beside `T4.2` and record the split.

An undocumented finish is an unfinished task. The handoff log entry belongs to the task. Write
what exists now, what surprised you, and what the next agent should not work out again.

## Stack, frozen

Go 1.27.1, standard library first. Svelte 5 with runes on Vite, Node 26. ffmpeg n9.0.1, which
supplies `atempo`, `amix`, `aresample`, and `sidechaincompress`.

One exception applies. google.golang.org/genai supplies the Gemini clients.
The SDK authenticates with Application Default Credentials and covers the GCE metadata server.

ffmpeg does all audio work. We add no third-party AI audio dependency.

### Svelte 5 only, never mixed

Use runes: `$state`, `$derived`, `$props`, `$effect`. Never write `export let`. Never write
`$:`. Never use a store to drive reactivity.

Mixing versions carries the risk, not runes themselves. A Svelte 4 idiom inside a Svelte 5 file
compiles clean and then never reacts. You get no error and no warning.

The wrong code looks right on the page. It stays wrong until someone changes state and nothing
moves. Reviewers on the workspace track grep for version leakage before they read.

## Documentation style

Four rules. They cover markdown, code comments, and commit messages alike.

1. No semicolons. Split the sentence.
2. No em dashes. Use a full stop, a comma, or parentheses.
3. Sentences run 30 words at most.
4. Active voice, unless passive genuinely reads clearer.

Check before you write rather than after. Prose that breaks these rules invites a rewrite, and a
rewrite loses whatever it does not understand.

## Secrets

Keep secrets out of the repository and out of every image layer. Never export one in an
interactive shell, because that writes it to `~/.zsh_history`. That path leaks in practice.

Required secrets carry no default value. A missing one stops startup with an error naming the
variable. `.env` stays gitignored.

## Memory, where available

If this machine exposes the lambo MCP server, call `lambo_recall` before you start and
`lambo_derive` plus `lambo_record_action` after each meaningful change. Use one stable
`agent_id` for the session so locks and attribution stay coherent.

Treat lambo as an addition rather than a prerequisite. Some agents lack access, and another
machine carries a different graph. This file holds everything you need on its own.

## Read order

1. This file, in full.
2. Your task's block in its `dev-diary/PHASE-*.md`.
3. The conventions in [`dev-diary/README.md`](dev-diary/README.md).
4. Any prior review rounds for your task.
5. The code you will edit, and its tests.

Then assert three things. I own every path I will edit. The shapes I need already exist. I can
validate this task on its own. If any part reads false, stop and propose a contract change in
the review file.
