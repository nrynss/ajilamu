<script lang="ts">
  import { onMount, tick } from "svelte"
  import { page } from "$app/state"
  import {
    fixtureLineRows,
    fixtureTakeSource,
    loadFixtureDub,
    pictureDurationMs,
    referenceSlotMs
  } from "$lib/fixture"
  import LanguageStrip from "$lib/LanguageStrip.svelte"
  import LengthBar from "$lib/LengthBar.svelte"
  import Preview from "$lib/Preview.svelte"
  import type { PlaybackSnapshot } from "$lib/Preview.svelte"
  import Rail from "$lib/Rail.svelte"
  import { PanelState, type PanelViewState } from "$lib/states"
  import { createShortcutManager } from "$lib/shortcuts"
  import Timeline from "$lib/Timeline.svelte"
  import type { Dub, Take } from "$lib/types"

  type SelectionSource = "user" | "playhead"

  const loadingSentence = "We are loading this workspace from the offline project fixture."

  let projectID = $derived(page.params.id ?? "fixture")
  let initialDub: Dub | undefined
  let initialState: PanelViewState
  if (page.url.searchParams.get("panel") === "loading") {
    initialDub = undefined
    initialState = { kind: "loading", sentence: loadingSentence }
  } else {
    try {
      initialDub = loadFixtureDub(page.params.id ?? "fixture")
      initialState = initialDub.segments.length > 0 && initialDub.languages.length > 0
        ? { kind: "populated" }
        : { kind: "empty", sentence: "This project does not have any lines or dubbed tracks yet." }
    } catch {
      initialDub = undefined
      initialState = {
        kind: "error",
        sentence: "We could not load this offline project, so no workspace data was changed."
      }
    }
  }

  let dub = $state<Dub | undefined>(initialDub)
  let workspaceState = $state<PanelViewState>(initialState)
  let selectedSegmentId = $state(initialDub?.segments[0]?.id ?? 0)
  let activeLanguage = $state(initialDub?.languages[0]?.language ?? "")
  let theme = $state("light")
  let playback = $state<PlaybackSnapshot>({ currentTime: 0, duration: 0, paused: true })
  let helpDialog = $state<HTMLDialogElement | undefined>()
  let commandBar = $state<HTMLInputElement | undefined>()
  let commandStatus = $state("")
  let preview = $state<Preview | undefined>()
  let lengthList = $state<HTMLOListElement | undefined>()
  let takeAudio: HTMLAudioElement | undefined

  let lineRows = $derived(dub ? fixtureLineRows(dub, activeLanguage) : [])
  let selectedRow = $derived(lineRows.find((row) => row.segment.id === selectedSegmentId))
  let selectedTake = $derived(selectedRow?.take)
  let sharedReferenceSlotMs = $derived(dub ? referenceSlotMs(dub) : 1)
  let sharedPictureDurationMs = $derived(dub ? pictureDurationMs(dub) : 0)
  let flaggedNotes = $derived(lineRows.filter((row) => row.line?.flagged).map(flagSentence))
  let railDub = $derived.by((): Dub | undefined => {
    if (!dub) return undefined
    return {
      ...dub,
      languages: dub.languages.filter((track) => track.language === activeLanguage)
    }
  })

  function flagSentence(row: (typeof lineRows)[number]): string {
    const take = row.take
    if (!take) return `Line ${row.segment.id} still needs a take.`

    const seconds = (Math.abs(take.fit.delta_ms) / 1000).toFixed(2)
    const tries = row.line?.takes.length ?? 0
    const tryWord = tries === 1 ? "try" : "tries"

    if (take.fit.state === "too_short") {
      return `Line ${row.segment.id} is still ${seconds} seconds too short after ${tries} ${tryWord}, this one needs you.`
    }

    if (take.fit.state === "too_long") {
      return `Line ${row.segment.id} is still ${seconds} seconds too long after ${tries} ${tryWord}, this one needs you.`
    }

    return `Line ${row.segment.id} still needs you.`
  }

  function revealSelectedLengthRow(): void {
    const list = lengthList
    if (!list) return
    const row = list.querySelector<HTMLElement>(`#length-line-${selectedSegmentId}`)
    if (!row) return

    const rowTop = row.offsetTop
    const rowBottom = rowTop + row.offsetHeight
    const viewTop = list.scrollTop
    const viewBottom = viewTop + list.clientHeight

    if (rowTop < viewTop) {
      list.scrollTop = rowTop
    } else if (rowBottom > viewBottom) {
      list.scrollTop = rowBottom - list.clientHeight
    }
  }

  async function selectSegment(segmentID: number, source: SelectionSource = "user"): Promise<void> {
    if (!dub?.segments.some((segment) => segment.id === segmentID)) return
    selectedSegmentId = segmentID
    await tick()
    revealSelectedLengthRow()
    if (source !== "user") return
    const segment = dub.segments.find((candidate) => candidate.id === segmentID)
    if (segment) preview?.seek(segment.start_ms / 1000)
  }

  function moveSelection(direction: "previous" | "next" | "left" | "right" | "up" | "down"): void {
    const segments = dub?.segments ?? []
    const currentIndex = Math.max(0, segments.findIndex((segment) => segment.id === selectedSegmentId))
    const step = direction === "previous" || direction === "left" || direction === "up" ? -1 : 1
    const next = segments[Math.min(segments.length - 1, Math.max(0, currentIndex + step))]
    if (next) selectSegment(next.id)
  }

  function stopTake(): void {
    takeAudio?.pause()
    takeAudio = undefined
  }

  function togglePlayback(): void {
    stopTake()
    if (playback.paused) void preview?.play().catch(() => undefined)
    else preview?.pause()
  }

  function playTake(take: Take): void {
    preview?.pause()
    stopTake()
    const source = fixtureTakeSource(take)
    if (!source) return
    const audio = new Audio(source)
    takeAudio = audio
    audio.addEventListener("ended", () => {
      if (takeAudio === audio) takeAudio = undefined
    }, { once: true })
    void audio.play().catch(() => undefined)
  }

  function handlePlaybackChange(snapshot: PlaybackSnapshot): void {
    playback = snapshot
    if (snapshot.paused || !dub) return
    const ms = snapshot.currentTime * 1000
    const hit = dub.segments.find((segment) => ms >= segment.start_ms && ms < segment.end_ms)
    if (hit) selectSegment(hit.id, "playhead")
  }

  function showHelp(): void {
    if (helpDialog && !helpDialog.open) helpDialog.showModal()
  }

  function dismissModal(): void {
    if (helpDialog?.open) helpDialog.close()
  }

  function focusCommandBar(): void {
    commandBar?.focus()
  }

  function handleCommand(event: SubmitEvent): void {
    event.preventDefault()
    commandStatus = "This workspace cannot run editor commands yet."
  }

  onMount(() => {
    const updateTheme = () => {
      theme = document.documentElement.dataset.theme ?? "light"
    }
    const themeObserver = new MutationObserver(updateTheme)
    updateTheme()
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] })

    const manager = createShortcutManager({
      showHelp,
      focusCommandBar,
      togglePlayback,
      moveSelection,
      dismissModal
    })
    manager.start()

    return () => {
      manager.stop()
      themeObserver.disconnect()
      stopTake()
    }
  })
</script>

<svelte:head>
  <title>{dub ? `${dub.title} · Ajilamu` : "Ajilamu · Workspace"}</title>
</svelte:head>

<div class="page">
  <PanelState state={workspaceState} label="Dubbing workspace">
    {#if dub}
        <section class="workspace" aria-label={`${dub.title} dubbing workspace`} data-project-id={projectID}>
        <section class="picture-area" aria-label="Picture and playback">
          <h1 class="project-title">{dub.title}</h1>
          <Preview
            bind:this={preview}
            src="/clip.mp4"
            title={dub.title}
            durationMs={sharedPictureDurationMs}
            {activeLanguage}
            enableSpaceShortcut={false}
            onplaybackchange={handlePlaybackChange}
          />
          <LanguageStrip
            languages={dub.languages}
            {activeLanguage}
            onlanguagechange={(language) => activeLanguage = language}
          />
          <p class="playback-note">
            The picture plays its original audio. Play discrete takes from the length checks or Lines rail.
          </p>
          {#if flaggedNotes.length > 0}
            <section class="flagged-lines" aria-labelledby="flagged-lines-title">
              <p class="label" id="flagged-lines-title">Lines needing you</p>
              {#each flaggedNotes as note, index (index)}
                <p class="flag" role="status">{note}</p>
              {/each}
            </section>
          {/if}
          <section class="length-checks" aria-label="Length checks">
            <div class="length-heading">
              <div>
                <p class="label">Length checks</p>
                <h2>Line <span class="numeric">{selectedSegmentId}</span></h2>
              </div>
              {#if selectedTake}
                <button type="button" class="take-play" onclick={() => playTake(selectedTake)}>
                  Play this take
                </button>
              {/if}
            </div>
            <ol class="length-list" bind:this={lengthList}>
              {#each lineRows as row (row.segment.id)}
                {@const take = row.take}
                <li
                  class:selected={row.segment.id === selectedSegmentId}
                  data-segment-id={row.segment.id}
                  id={`length-line-${row.segment.id}`}
                >
                  <div class="length-row-head">
                    <button
                      type="button"
                      class="line-select numeric"
                      aria-current={row.segment.id === selectedSegmentId ? "true" : undefined}
                      onclick={() => selectSegment(row.segment.id)}
                    >
                      Line {row.segment.id}
                    </button>
                    {#if take}
                      <button type="button" class="take-play" onclick={() => playTake(take)}>
                        Play take
                      </button>
                    {/if}
                  </div>
                  {#if row.line}
                    <LengthBar
                      slotMs={row.segment.duration_ms}
                      takes={row.line.takes}
                      referenceSlotMs={sharedReferenceSlotMs}
                      segmentId={row.segment.id}
                      {theme}
                    />
                  {:else}
                    <p class="no-take">No take is ready for this line.</p>
                  {/if}
                </li>
              {/each}
            </ol>
          </section>
        </section>

        {#if railDub}
          <Rail dub={railDub} {selectedSegmentId} onselect={selectSegment} onplaytake={playTake} />
        {/if}

        <section class="editor" aria-label="Timeline editor">
          <form class="command-bar" onsubmit={handleCommand}>
            <label for="editor-command">Editor AI command</label>
            <input
              bind:this={commandBar}
              id="editor-command"
              autocomplete="off"
              placeholder="Type a command to edit this timeline"
            />
            <span class="hint numeric">Press / to focus</span>
          </form>
          {#if commandStatus}
            <p class="command-status" role="status">{commandStatus}</p>
          {/if}
          <Timeline
            segments={dub.segments}
            tracks={dub.languages}
            durationMs={sharedPictureDurationMs}
          />
        </section>
      </section>
    {/if}
  </PanelState>
</div>

<dialog bind:this={helpDialog} aria-labelledby="shortcut-title">
  <div class="help-heading">
    <h2 id="shortcut-title">Keyboard shortcuts</h2>
    <button type="button" onclick={dismissModal}>Close</button>
  </div>
  <dl>
    <div>
      <dt class="numeric">?</dt>
      <dd>Open this shortcut list.</dd>
    </div>
    <div>
      <dt class="numeric">/</dt>
      <dd>Focus the editor command bar.</dd>
    </div>
    <div>
      <dt class="numeric">Space</dt>
      <dd>Play or pause the picture.</dd>
    </div>
    <div>
      <dt class="numeric">[ and ]</dt>
      <dd>Move to the previous or next line.</dd>
    </div>
    <div>
      <dt class="numeric">Arrow keys</dt>
      <dd>Move the selected line.</dd>
    </div>
    <div>
      <dt class="numeric">Escape</dt>
      <dd>Close this dialog.</dd>
    </div>
  </dl>
</dialog>

<style>
  .page :global(.panel-state[data-state="loading"]),
  .page :global(.panel-state[data-state="empty"]),
  .page :global(.panel-state[data-state="error"]) {
    padding: 20px;
  }

  .workspace {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 352px;
    grid-template-rows: minmax(0, 1fr) auto;
    height: calc(100vh - 46px);
    min-height: 620px;
  }

  .picture-area {
    min-width: 0;
    overflow: auto;
    padding: 20px 20px 14px;
  }

  .project-title {
    font-size: 14px;
    margin: 0 0 10px;
  }

  .playback-note {
    color: var(--dim);
    font-size: 11.5px;
    margin: 8px 0 0;
  }

  .flagged-lines {
    margin-top: 14px;
  }

  .flagged-lines > .label {
    margin: 0 0 6px;
  }

  .length-checks {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
    margin-top: 14px;
    padding: 10px 12px;
  }

  .length-heading {
    align-items: center;
    display: flex;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  .length-heading p,
  .length-heading h2,
  .no-take,
  .flag {
    margin: 0;
  }

  .length-heading h2 {
    font-size: 13px;
    margin-top: 2px;
  }

  .flag {
    background: var(--accent-q);
    border: 1px solid var(--accent);
    border-radius: var(--radius-control);
    color: var(--text);
    font-size: 12.5px;
    margin-bottom: 8px;
    padding: 8px 10px;
  }

  .take-play {
    font-size: 11.5px;
    padding: 4px 7px;
  }

  .length-list {
    list-style: none;
    margin: 0;
    max-height: min(420px, 46vh);
    overflow: auto;
    padding: 0;
  }

  .length-list li {
    border-top: 1px solid var(--line-soft);
    padding: 8px 6px 6px;
  }

  .length-list li.selected {
    background: var(--accent-q);
    border-radius: var(--radius-control);
  }

  .length-row-head {
    align-items: center;
    display: flex;
    justify-content: space-between;
    margin-bottom: 4px;
  }

  .line-select {
    background: transparent;
    border: 0;
    color: var(--dim);
    font-size: 11.5px;
    padding: 0;
  }

  .length-list li.selected .line-select {
    color: var(--accent);
  }

  .no-take {
    color: var(--dim);
    font-size: 11.5px;
  }

  .editor {
    background: var(--surface);
    border-top: 1px solid var(--line);
    grid-column: 1 / -1;
    min-height: 156px;
  }

  .command-bar {
    align-items: center;
    border-bottom: 1px solid var(--line-soft);
    display: grid;
    gap: 10px;
    grid-template-columns: 116px minmax(0, 1fr) auto;
    padding: 8px 16px;
  }

  .command-bar label {
    color: var(--dim);
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .command-bar input {
    background: var(--raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    color: var(--text);
    font: inherit;
    min-width: 0;
    padding: 6px 8px;
  }

  .hint {
    color: var(--faint);
    font-size: 10px;
  }

  .command-status {
    color: var(--dim);
    font-size: 11.5px;
    margin: 0;
    padding: 6px 16px;
  }

  dialog {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-container);
    box-shadow: 0 14px 34px rgb(0 0 0 / 20%);
    color: var(--text);
    max-width: 400px;
    padding: 16px;
  }

  dialog::backdrop {
    background: rgb(0 0 0 / 32%);
  }

  .help-heading {
    align-items: center;
    display: flex;
    justify-content: space-between;
  }

  .help-heading h2 {
    font-size: 14px;
    margin: 0;
  }

  .help-heading button {
    padding: 4px 7px;
  }

  dialog dl {
    margin: 14px 0 0;
  }

  dialog dl div {
    border-top: 1px solid var(--line-soft);
    display: grid;
    gap: 12px;
    grid-template-columns: 70px 1fr;
    padding: 8px 0;
  }

  dialog dt,
  dialog dd {
    margin: 0;
  }

  dialog dd {
    color: var(--dim);
    font-size: 11.5px;
  }

  @media (max-width: 780px) {
    .workspace {
      grid-template-columns: 1fr;
      grid-template-rows: auto auto minmax(156px, 42vh);
      height: auto;
    }

    .picture-area {
      padding: 16px;
    }

    .editor {
      grid-column: 1;
    }
  }

  @media (max-width: 560px) {
    .command-bar {
      grid-template-columns: 1fr;
    }

    .hint {
      display: none;
    }
  }
</style>
