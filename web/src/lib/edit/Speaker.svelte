<script lang="ts">
  import type { Segment, Take } from "$lib/types"

  export interface SpeakerChange {
    segment: Segment
    previousSpeaker: string
    speaker: string
    /** EstimatedNanodollars sums the charges on the line's newest billed take, or zero when none is billed. */
    estimatedNanodollars: number
    /** TakeFile names the take that the estimate came from. */
    takeFile: string
  }

  /** SpeakerChangeReply reports the take the re-render route recorded. */
  export interface SpeakerChangeReply {
    sentence: string
    take: Take
    totalNanodollars: number
  }

  /** SpeakerChangeFailure explains why the line did not re-render. */
  export interface SpeakerChangeFailure {
    error: string
  }

  export type SpeakerChangeResult = SpeakerChangeReply | SpeakerChangeFailure

  interface Props {
    /** The timeline segment whose speaker the editor changes. */
    segment: Segment
    /** Every source segment supplies the speaker choices. */
    segments: readonly Segment[]
    /** The selected line's takes supply the measured estimate. */
    takes?: readonly Take[]
    /** The parent calls the re-render route and returns its reply. */
    onchange?: (change: SpeakerChange) => Promise<SpeakerChangeResult>
  }

  let { segment, segments, takes = [], onchange }: Props = $props()

  let pendingSpeaker = $state<string | undefined>()
  // svelte-ignore state_referenced_locally
  let appliedSpeaker = $state(segment.speaker)
  let note = $state("")
  let renderingSegmentIds = $state<Set<number>>(new Set())
  const busy = $derived(renderingSegmentIds.has(segment.id))
  let error = $state("")
  let sourceSignature = $state("")

  const lastTake = $derived(takes.at(-1))
  const estimateTake = $derived([...takes].reverse().find((take) => take.charges.length > 0) ?? lastTake)
  const estimatedNanodollars = $derived.by(() => (
    (estimateTake?.charges ?? []).reduce((sum, charge) => sum + charge.total_nanodollars, 0)
  ))
  const feeText = $derived(formatFee(estimatedNanodollars))
  const speakers = $derived.by(() => {
    const names = new Set<string>()
    for (const candidate of segments) {
      const name = candidate.speaker.trim()
      if (name.length > 0) names.add(name)
    }
    if (appliedSpeaker.length > 0) names.add(appliedSpeaker)
    return [...names].sort()
  })

  $effect(() => {
    const next = `${segment.id}:${segment.speaker}`
    if (next === sourceSignature) return
    sourceSignature = next
    appliedSpeaker = segment.speaker
    pendingSpeaker = undefined
    note = ""
    error = ""
  })

  function formatFee(nanodollars: number): string {
    const dollars = nanodollars / 1_000_000_000
    const fixed = dollars.toFixed(9).replace(/0+$/, "")
    const trimmed = fixed.endsWith(".") ? fixed.slice(0, -1) : fixed
    const [whole, fraction = ""] = trimmed.split(".")
    return `$${whole}.${fraction.padEnd(2, "0")}`
  }

  function choose(name: string): void {
    note = ""
    error = ""
    pendingSpeaker = name === appliedSpeaker ? undefined : name
  }

  function cancel(): void {
    pendingSpeaker = undefined
    note = ""
    error = ""
  }

  async function confirm(): Promise<void> {
    const choice = pendingSpeaker
    const take = estimateTake
    const targetSegment = segment
    if (choice === undefined || take === undefined) return
    if (!onchange) {
      error = "Line re-rendering is unavailable for this workspace."
      return
    }

    // The panel shows the choice before the route answers, so a failure must
    // put the stored speaker back.
    const previousSpeaker = appliedSpeaker
    const previousSignature = sourceSignature
    const next = { ...segment, speaker: choice }
    appliedSpeaker = choice
    pendingSpeaker = undefined
    sourceSignature = `${segment.id}:${choice}`
    note = ""
    error = ""
    renderingSegmentIds = new Set(renderingSegmentIds).add(segment.id)
    try {
      const result = await onchange({
        segment: next,
        previousSpeaker: segment.speaker,
        speaker: choice,
        estimatedNanodollars,
        takeFile: take.file
      })
      if (segment.id !== targetSegment.id) return
      if ("error" in result) {
        appliedSpeaker = previousSpeaker
        sourceSignature = previousSignature
        error = result.error
        return
      }
      note = result.sentence || `Line ${targetSegment.id} re-rendered as ${result.take.file}.`
    } catch {
      appliedSpeaker = previousSpeaker
      sourceSignature = previousSignature
      error = "We could not re-render that line. The take is unchanged."
    } finally {
      const settled = new Set(renderingSegmentIds)
      settled.delete(targetSegment.id)
      renderingSegmentIds = settled
    }
  }
</script>

<section
  class="speaker-editor"
  aria-label={`Reassign speaker for line ${segment.id}`}
  data-take-files={takes.map((take) => take.file).join(",")}
>
  <div class="editor-heading">
    <div>
      <p class="eyebrow">Line {segment.id}</p>
      <h2>Speaker</h2>
    </div>
    <p class="numeric" data-speaker={appliedSpeaker}>{appliedSpeaker}</p>
  </div>

  <div class="speaker-toggles" role="group" aria-label={`Speakers for line ${segment.id}`}>
    {#each speakers as name (name)}
      <button
        aria-pressed={name === appliedSpeaker}
        class:current={name === appliedSpeaker}
        data-speaker={name}
        onclick={() => choose(name)}
        type="button"
      >{name}</button>
    {/each}
  </div>

  {#if takes.length > 0}
    <p class="takes">
      Takes on this line:
      {#each takes as take, takeIndex (`${take.file}-${takeIndex}`)}
        <span class="numeric" data-take={take.file}>{take.file}</span>
      {/each}
    </p>
  {/if}

  {#if pendingSpeaker !== undefined}
    <aside aria-live="assertive" class="pending" role="alert">
      <p>
        Re-rendering line {segment.id} as {pendingSpeaker} is estimated at
        <span class="numeric" data-fee={estimatedNanodollars} data-fee-text={feeText}>{feeText}</span>.
      </p>
      {#if estimateTake}
        <p class="basis">
          Estimated from the take
          <span class="numeric" data-take-file={estimateTake.file}>{estimateTake.file}</span>.
        </p>
      {:else}
        <p class="basis">No take is recorded for this line, so no estimate exists.</p>
      {/if}
      <div class="actions">
        <button onclick={cancel} type="button" disabled={busy}>Cancel</button>
        <button class="confirm" disabled={!estimateTake || busy} onclick={confirm} type="button">
          {busy ? "Re-rendering…" : "Change speaker"}
        </button>
      </div>
    </aside>
  {/if}

  {#if note}
    <p class="note" role="status">{note}</p>
  {/if}

  {#if error}
    <p class="error" role="alert">{error}</p>
  {/if}

  <p class="hint">A new speaker needs a fresh voice render. The estimate uses the newest billed take, or reads zero when none is billed.</p>
</section>

<style>
  .speaker-editor {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-card);
    color: var(--text);
    padding: 16px;
  }

  .editor-heading {
    align-items: start;
    display: flex;
    gap: 16px;
    justify-content: space-between;
  }

  .editor-heading h2,
  .editor-heading p {
    margin: 0;
  }

  .editor-heading h2 {
    font-size: 15px;
  }

  .editor-heading .numeric {
    color: var(--dim);
    font-size: 11px;
    padding-top: 3px;
  }

  .eyebrow {
    color: var(--faint);
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .speaker-toggles {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 14px;
  }

  .speaker-toggles button {
    border: 1px solid var(--line);
    padding: 5px 9px;
  }

  .speaker-toggles button.current {
    background: var(--accent-q);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 650;
  }

  .takes {
    color: var(--dim);
    font-size: 11.5px;
    margin: 12px 0 0;
  }

  .takes span {
    color: var(--text);
    margin-left: 6px;
  }

  .pending {
    background: var(--accent-q);
    border: 1px solid var(--accent);
    border-radius: var(--radius-panel);
    display: grid;
    gap: 6px;
    margin-top: 12px;
    padding: 10px;
  }

  .pending p {
    font-size: 12.5px;
    margin: 0;
  }

  .basis {
    color: var(--dim);
    font-size: 11.5px;
  }

  .actions {
    display: flex;
    gap: 8px;
  }

  .actions button {
    padding: 5px 9px;
  }

  .actions .confirm {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--surface);
  }

  .note,
  .hint {
    color: var(--dim);
    font-size: 11.5px;
    margin: 12px 0 0;
  }

  .error {
    color: var(--stop);
    font-size: 11.5px;
    margin: 12px 0 0;
  }
</style>
