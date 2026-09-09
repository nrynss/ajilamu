<script lang="ts">
  export interface CommandIntent {
    command: string
    summary: string
  }

  export interface CommandParseFailure {
    error: string
    canAskAgent?: boolean
  }

  export interface AgentTurn {
    answer: string
    totalNanodollars: number
  }

  export interface Props {
    /** The server parser validates the proposed mutation before this preview appears. */
    parse?: (command: string) => Promise<CommandIntent | CommandParseFailure>
    /** The editor agent answers a parser-rejected instruction without applying an edit. */
    askagent?: (question: string) => Promise<AgentTurn | CommandParseFailure>
    /** The parent applies a creator-confirmed intent and records its audit event. */
    onconfirm?: (intent: CommandIntent) => void | Promise<void>
    id?: string
    placeholder?: string
  }

  let {
    parse,
    askagent,
    onconfirm,
    id = "editor-command",
    placeholder = "Type a command to edit this timeline"
  }: Props = $props()

  let field = $state<HTMLInputElement | undefined>()
  let command = $state("")
  let intent = $state<CommandIntent | undefined>()
  let error = $state("")
  let canAskAgent = $state(false)
  let rejectedCommand = $state("")
  let agentTurn = $state<AgentTurn | undefined>()
  let submitting = $state(false)
  let askingAgent = $state(false)
  let confirming = $state(false)

  export function focus(): void {
    field?.focus()
  }

  function focusOnSlash(event: KeyboardEvent): void {
    if (event.key !== "/" || event.repeat || event.altKey || event.ctrlKey || event.metaKey || event.isComposing) return
    if (event.target instanceof HTMLElement && event.target.closest("input, textarea, select, [contenteditable]")) return
    event.preventDefault()
    focus()
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    const requested = command.trim()
    intent = undefined
    error = ""
    canAskAgent = false
    rejectedCommand = ""
    agentTurn = undefined

    if (!requested) {
      error = "Type an editor command before asking for a preview."
      return
    }
    if (!parse) {
      error = "Command validation is unavailable for this timeline."
      return
    }

    await preview(requested)
  }

  async function preview(requested: string): Promise<void> {
    const parser = parse
    if (!parser) {
      error = "Command validation is unavailable for this timeline."
      return
    }
    submitting = true
    try {
      const result = await parser(requested)
      if ("error" in result) {
        error = result.error
        canAskAgent = result.canAskAgent === true
        rejectedCommand = canAskAgent ? requested : ""
        return
      }
      canAskAgent = false
      rejectedCommand = ""
      intent = { ...result, command: requested }
    } catch {
      error = "We could not validate that command. No timeline change was made."
    } finally {
      submitting = false
    }
  }

  async function askEditorAgent(): Promise<void> {
    const question = rejectedCommand
    if (!question || !askagent) return
    askingAgent = true
    agentTurn = undefined
    try {
      const result = await askagent(question)
      if ("error" in result) {
        error = result.error
        return
      }
      error = ""
      canAskAgent = false
      rejectedCommand = ""
      agentTurn = result
    } catch {
      error = "We could not reach the editor agent. No timeline change was made."
    } finally {
      askingAgent = false
    }
  }

  async function previewAgentAnswer(): Promise<void> {
    const answer = agentTurn?.answer
    if (!answer) return
    command = answer
    intent = undefined
    error = ""
    canAskAgent = false
    rejectedCommand = ""
    await preview(answer)
  }

  async function confirm(): Promise<void> {
    const confirmed = intent
    if (!confirmed || !onconfirm) return
    confirming = true
    error = ""
    try {
      await onconfirm(confirmed)
      command = ""
      intent = undefined
      agentTurn = undefined
      field?.focus()
    } catch {
      error = "We could not apply that command. No timeline change was made."
    } finally {
      confirming = false
    }
  }

  function revise(): void {
    intent = undefined
    error = ""
    field?.focus()
  }

  function exactCharge(nanodollars: number): string {
    const dollars = nanodollars / 1_000_000_000
    const fixed = dollars.toFixed(9).replace(/0+$/, "")
    const trimmed = fixed.endsWith(".") ? fixed.slice(0, -1) : fixed
    const [whole, fraction = ""] = trimmed.split(".")
    return `$${whole}.${fraction.padEnd(2, "0")}`
  }
</script>

<svelte:window onkeydown={focusOnSlash} />

<section class="command-bar" aria-label="Editor command bar">
  <form onsubmit={submit}>
    <label for={id}>Editor command</label>
    <div class="entry">
      <input
        bind:this={field}
        bind:value={command}
        {id}
        autocomplete="off"
        {placeholder}
        aria-describedby={`${id}-hint ${error ? `${id}-error` : ""}`}
        disabled={submitting || askingAgent || confirming}
      />
      <button type="submit" disabled={submitting || askingAgent || confirming}>
        {submitting ? "Checking…" : "Preview"}
      </button>
    </div>
    <p class="hint numeric" id={`${id}-hint`}>Press / to focus</p>
  </form>

  {#if error}
    <p class="error" id={`${id}-error`} role="alert">{error}</p>
  {/if}

  {#if canAskAgent}
    <section class="agent-offer" aria-label="Ask the editor agent" aria-live="polite" role="status">
      <!-- These prices mirror cost.DefaultRateCard in internal/cost/cost.go. -->
      <p>The editor agent charges $0.00000015 for each prompt step that costs money and $0.00000060 for each answer step that costs money, then shows the measured charge after the turn.</p>
      <button type="button" onclick={askEditorAgent} disabled={askingAgent || !askagent}>
        {askingAgent ? "Asking…" : "Ask the editor agent"}
      </button>
    </section>
  {/if}

  {#if agentTurn}
    <section class="agent-answer" aria-atomic="true" aria-label="Editor agent answer" aria-live="polite" role="status">
      <p class="label">Editor agent</p>
      <p>{agentTurn.answer}</p>
      <p class="agent-charge">This turn cost <span class="numeric">{exactCharge(agentTurn.totalNanodollars)}</span>.</p>
      <button type="button" onclick={previewAgentAnswer} disabled={submitting}>
        {submitting ? "Checking…" : "Preview answer as command"}
      </button>
    </section>
  {/if}

  {#if intent}
    <section class="intent" aria-atomic="true" aria-label="Parsed command intent" aria-live="polite" role="status">
      <p class="label">Parsed intent</p>
      <p>{intent.summary}</p>
      <div class="actions">
        <button type="button" onclick={confirm} disabled={confirming || !onconfirm}>
          {confirming ? "Applying…" : "Confirm change"}
        </button>
        <button type="button" onclick={revise} disabled={confirming}>Revise</button>
      </div>
    </section>
  {/if}
</section>

<style>
  .command-bar {
    border-bottom: 1px solid var(--line-soft);
    padding: 8px 16px;
  }

  form {
    display: grid;
    gap: 4px;
  }

  label {
    color: var(--dim);
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .entry,
  .actions {
    align-items: center;
    display: flex;
    gap: 8px;
  }

  input {
    background: var(--raised);
    border: 1px solid var(--line);
    color: var(--text);
    min-width: 0;
    padding: 7px 9px;
    width: 100%;
  }

  button {
    padding: 7px 9px;
    white-space: nowrap;
  }

  .hint,
  .error,
  .agent-offer p,
  .agent-answer p,
  .intent p {
    font-size: 11.5px;
    margin: 0;
  }

  .hint {
    color: var(--dim);
  }

  .error {
    color: var(--stop);
    margin-top: 8px;
  }

  .intent {
    background: var(--accent-q);
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
    display: grid;
    gap: 6px;
    margin-top: 8px;
    padding: 9px;
  }

  .agent-offer,
  .agent-answer {
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
    display: grid;
    gap: 7px;
    margin-top: 8px;
    padding: 9px;
  }

  .agent-offer button,
  .agent-answer button {
    justify-self: start;
  }

  .agent-charge {
    color: var(--dim);
  }

  .intent .label {
    color: var(--dim);
  }

  @media (max-width: 560px) {
    .entry,
    .actions {
      align-items: stretch;
      flex-direction: column;
    }
  }
</style>
