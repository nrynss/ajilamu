<script lang="ts">
  import type { Line, Segment, Take } from "$lib/types"

  /** TextRerenderRequest asks the parent to re-render one line from corrected target text. */
  export interface TextRerenderRequest {
    segmentId: number
    /** Language names the target track the correction belongs to. */
    language: string
    text: string
  }

  /** TextRerenderResult reports the take the server recorded. */
  export interface TextRerenderResult {
    sentence: string
    take: Take
    totalNanodollars: number
  }

  /** TextRerenderFailure explains why the line did not re-render. */
  export interface TextRerenderFailure {
    error: string
  }

  export type TextRerenderReply = TextRerenderResult | TextRerenderFailure

  interface Props {
    /** Segment supplies the transcribed source sentence. */
    segment: Segment
    /** Line supplies the active target-language text and its takes. */
    line?: Line
    /** Language tags the translated element and rides the re-render body. */
    language: string
    /** SourceLanguage tags the source element when the project knows it. */
    sourceLanguage?: string
    /** The parent calls the re-render route and applies the new take. */
    onrerender?: (request: TextRerenderRequest) => Promise<TextRerenderReply>
  }

  let { segment, line, language, sourceLanguage = "", onrerender }: Props = $props()

  // svelte-ignore state_referenced_locally
  let sourceDraft = $state(segment.text)
  // svelte-ignore state_referenced_locally
  let targetDraft = $state(line?.text ?? "")
  // svelte-ignore state_referenced_locally
  let appliedSource = $state(segment.text)
  // svelte-ignore state_referenced_locally
  let appliedTarget = $state(line?.text ?? "")
  let pending = $state<"source" | "target" | undefined>()
  let busy = $state(false)
  let note = $state("")
  let error = $state("")
  // svelte-ignore state_referenced_locally
  let sourceSegmentId = $state(segment.id)
  let sourceSignature = $state("")

  const takes = $derived(line?.takes ?? [])
  const estimateTake = $derived([...takes].reverse().find((take) => take.charges.length > 0) ?? takes.at(-1))
  const estimatedNanodollars = $derived.by(() => (
    (estimateTake?.charges ?? []).reduce((sum, charge) => sum + charge.total_nanodollars, 0)
  ))
  const feeText = $derived(formatFee(estimatedNanodollars))
  const sourceDirty = $derived(sourceDraft !== appliedSource)
  const targetDirty = $derived(targetDraft !== appliedTarget)

  $effect(() => {
    const next = `${segment.id}:${segment.text}:${line?.text ?? ""}`
    if (next === sourceSignature) return
    // A prompt or a call keeps the draft only while the same line stays selected.
    if (segment.id === sourceSegmentId && (pending !== undefined || busy)) return
    sourceSegmentId = segment.id
    sourceSignature = next
    appliedSource = segment.text
    appliedTarget = line?.text ?? ""
    sourceDraft = appliedSource
    targetDraft = appliedTarget
    pending = undefined
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

  function promptSource(): void {
    error = ""
    note = ""
    pending = "source"
  }

  function promptTarget(): void {
    error = ""
    note = ""
    pending = "target"
  }

  function cancel(): void {
    pending = undefined
    error = ""
  }

  async function confirmTarget(): Promise<void> {
    const targetSegment = segment
    const targetLanguage = language
    const text = targetDraft.trim()
    if (text.length === 0) {
      error = "The corrected target line cannot be empty."
      return
    }
    if (!onrerender) {
      error = "Line re-rendering is unavailable for this workspace."
      return
    }

    busy = true
    error = ""
    try {
      const result = await onrerender({ segmentId: targetSegment.id, language: targetLanguage, text })
      if (segment.id !== targetSegment.id) {
        pending = undefined
        return
      }
      if ("error" in result) {
        error = result.error
        return
      }
      appliedTarget = text
      targetDraft = text
      sourceSignature = `${targetSegment.id}:${targetSegment.text}:${text}`
      pending = undefined
      note = result.sentence || `Line ${targetSegment.id} re-rendered as ${result.take.file}.`
    } catch {
      error = "We could not re-render that line. The take is unchanged."
    } finally {
      busy = false
    }
  }

  function confirmSource(): void {
    pending = undefined
    error = ""
    // The landed re-render route accepts a corrected target line only.
    // A corrected source needs a route that runs translation first, so this
    // panel reports the gap instead of sending a target text that would speak
    // the wrong language.
    note = `Line ${segment.id} cannot re-translate yet. The re-render route accepts a corrected target line, not a corrected source line. Nothing was sent and no model ran. T6.5b owns that path.`
  }
</script>

<section
  class="text-editor"
  aria-label={`Correct text for line ${segment.id}`}
  data-selected-line={segment.id}
  data-take-files={takes.map((take) => take.file).join(",")}
>
  <div class="editor-heading">
    <div>
      <p class="eyebrow">Line {segment.id}</p>
      <h2>Text</h2>
    </div>
    <p class="numeric" data-estimate-basis={estimateTake?.file ?? ""}>{estimateTake?.file ?? "no take"}</p>
  </div>

  <div class="field">
    <label for={`source-text-${segment.id}`}>Source line</label>
    <textarea
      bind:value={sourceDraft}
      data-source-lang={sourceLanguage}
      data-source-text={segment.id}
      disabled={busy}
      id={`source-text-${segment.id}`}
      lang={sourceLanguage || undefined}
      rows="3"
    ></textarea>
    <div class="field-actions">
      <button type="button" onclick={promptSource} disabled={!sourceDirty || busy}>Re-translate line</button>
      <span class="hint">A corrected source line needs re-translation. T6.5b lands that path.</span>
    </div>
  </div>

  <div class="field">
    <label for={`target-text-${segment.id}`}>Target line</label>
    <textarea
      bind:value={targetDraft}
      data-target-lang={language}
      data-target-text={segment.id}
      disabled={busy}
      id={`target-text-${segment.id}`}
      lang={language}
      rows="3"
    ></textarea>
    <div class="field-actions">
      <button type="button" onclick={promptTarget} disabled={!targetDirty || busy}>Save and re-render</button>
      <span class="hint">Your target text is authoritative, so no translation runs.</span>
    </div>
  </div>

  {#if pending === "target"}
    <aside aria-live="assertive" class="pending" role="alert">
      <p>
        Re-rendering line {segment.id} from this corrected target text runs speech synthesis only.
        The text is authoritative, so no translation model runs.
      </p>
      {#if estimateTake}
        <p class="basis">
          Estimated at
          <span class="numeric" data-fee={estimatedNanodollars} data-fee-text={feeText}>{feeText}</span>
          from the take
          <span class="numeric" data-take-file={estimateTake.file}>{estimateTake.file}</span>.
        </p>
      {:else}
        <p class="basis">No take is recorded for this line, so no estimate exists.</p>
      {/if}
      <div class="actions">
        <button type="button" onclick={cancel} disabled={busy}>Cancel</button>
        <button class="confirm" type="button" onclick={confirmTarget} disabled={busy}>
          {busy ? "Re-rendering…" : "Save and re-render"}
        </button>
      </div>
    </aside>
  {/if}

  {#if pending === "source"}
    <aside aria-live="assertive" class="pending" role="alert">
      <p>
        Re-translating line {segment.id} from the corrected source needs a route that runs
        the translation model first. The landed re-render route accepts a corrected target
        line only, so no handler takes a corrected source line yet. T6.5b owns that path.
        This confirm reports the gap only, so it sends nothing and runs no model.
      </p>
      <div class="actions">
        <button type="button" onclick={cancel} disabled={busy}>Cancel</button>
        <button class="confirm" type="button" onclick={confirmSource} disabled={busy}>Re-translate line</button>
      </div>
    </aside>
  {/if}

  {#if error}
    <p class="error" role="alert">{error}</p>
  {/if}

  {#if note}
    <p class="note" role="status">{note}</p>
  {/if}
</section>

<style>
  .text-editor {
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

  .field {
    display: grid;
    gap: 4px;
    margin-top: 14px;
  }

  .field label {
    color: var(--dim);
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .field textarea {
    background: var(--raised);
    border: 1px solid var(--line);
    color: var(--text);
    font: inherit;
    min-height: 62px;
    padding: 7px 9px;
    resize: vertical;
    width: 100%;
  }

  .field-actions {
    align-items: center;
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .field-actions button {
    padding: 5px 9px;
  }

  .hint {
    color: var(--dim);
    font-size: 11.5px;
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

  .error {
    color: var(--stop);
    font-size: 11.5px;
    margin: 12px 0 0;
  }

  .note {
    color: var(--dim);
    font-size: 11.5px;
    margin: 12px 0 0;
  }
</style>
