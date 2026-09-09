<script lang="ts">
  import { onDestroy, onMount, tick } from "svelte"
  import { page } from "$app/state"
  import {
    fixtureLineRows,
    fixtureTakeSource,
    isFixtureID,
    loadFixtureDub,
    pictureDurationMs,
    referenceSlotMs
  } from "$lib/fixture"
  import Boundary, { type BoundaryChange } from "$lib/edit/Boundary.svelte"
  import Speaker, { type SpeakerChange, type SpeakerChangeResult } from "$lib/edit/Speaker.svelte"
  import Text, { type TextRerenderReply } from "$lib/edit/Text.svelte"
  import CommandBar, {
    type AgentTurn,
    type CommandIntent,
    type CommandParseFailure
  } from "$lib/edit/CommandBar.svelte"
  import { watchRunProgress, type ProgressSubscription } from "$lib/progress"
  import LanguageStrip from "$lib/LanguageStrip.svelte"
  import LengthBar from "$lib/LengthBar.svelte"
  import Preview from "$lib/Preview.svelte"
  import type { PlaybackSnapshot } from "$lib/Preview.svelte"
  import Rail from "$lib/Rail.svelte"
  import { PanelState, ProcessingBanner, type PanelViewState, type ProcessingStep } from "$lib/states"
  import { createShortcutManager } from "$lib/shortcuts"
  import Timeline from "$lib/Timeline.svelte"
  import type { AgentResponse, Charge, Dub, Fit, ProgressEvent, RepairKind, Segment, Take } from "$lib/types"

  type SelectionSource = "user" | "playhead"

  interface CommandPreview {
    command: string
    summary: string
    segment: CommandPreviewSegment
  }

  interface CommandPreviewSegment {
    id: number
    start_ms: number
    end_ms: number
    duration_ms: number
    speaker: string
  }

  interface RerenderTakePayload {
    file: string
    name: string
    attempt: number
    voice: string
    repair: string
    flagged: boolean
    fit: Fit
    charges: Charge[]
  }

  interface RerenderPayload {
    segment_id: number
    language: string
    sentence: string
    total_nanodollars: number
    text: string
    source_text?: string
    take: RerenderTakePayload
  }

  interface EditSegment {
    id: number
    start_ms: number
    end_ms: number
    duration_ms: number
    speaker: string
  }

  interface EditPayload {
    commit_id: string
    action: string
    author: string
    segment: EditSegment
    sentence: string
  }

  interface EditRequest {
    kind: "boundary" | "command"
    language: string
    segmentId?: number
    startMs?: number
    endMs?: number
    command?: string
    durationMs?: number
    allowOverlap?: boolean
  }

  interface LineRerenderRequest {
    segmentId: number
    language: string
    text?: string
    sourceText?: string
    speaker?: string
  }

  const loadingSentence = "We are loading this workspace from the offline project fixture."
  const pendingProjectSentence = "This project is waiting for dubbing. Its video is saved, but no lines, takes, or costs exist yet."
  const missingProjectSentence = "We could not find this project. Check the URL or return to the project index."
  const lookupFailedSentence = "We could not load the project list. Refresh this page or return to the index."

  const ledgerUnavailableSentence = "We could not reach the project ledger, so this workspace cannot load."

  const runUnavailableSentence = "Runs are unavailable, so this project cannot start."
  const runNoSourceSentence = "This project has no source video, so the run cannot start."
  const runStartFailedSentence = "We could not reach the server, so the dubbing run did not start."
  const runStartGenericSentence = "The dubbing run did not start, so nothing is processing."

  const fixtureReadOnlySentence = "This offline fixture cannot save an edit."

  let projectID = $derived(page.params.id ?? "fixture")
  let readOnly = $derived(isFixtureID(projectID))
  let initialDub: Dub | undefined
  let initialState: PanelViewState
  if (page.url.searchParams.get("panel") === "loading") {
    initialDub = undefined
    initialState = { kind: "loading", sentence: loadingSentence }
  } else if (isFixtureID(page.params.id ?? "fixture")) {
    try {
      initialDub = loadFixtureDub(page.params.id ?? "fixture")
      initialState = initialDub && initialDub.segments.length > 0 && initialDub.languages.length > 0
        ? { kind: "populated" }
        : {
            kind: "empty",
            sentence: pendingProjectSentence
          }
    } catch {
      initialDub = undefined
      initialState = {
        kind: "error",
        sentence: "We could not load this offline project, so no workspace data was changed."
      }
    }
  } else {
    initialDub = undefined
    initialState = {
      kind: "loading",
      sentence: "We are checking this project workspace."
    }
  }

  let dub = $state<Dub | undefined>(initialDub)
  let workspaceState = $state<PanelViewState>(initialState)
  let selectedSegmentId = $state(initialDub?.segments[0]?.id ?? 0)
  let activeLanguage = $state(initialDub?.languages[0]?.language ?? "")
  let theme = $state("light")
  let playback = $state<PlaybackSnapshot>({ currentTime: 0, duration: 0, paused: true })
  let helpDialog = $state<HTMLDialogElement | undefined>()
  let commandBar = $state<CommandBar | undefined>()
  let commandPreview = $state<CommandPreview | undefined>()
  let preview = $state<Preview | undefined>()
  let lengthList = $state<HTMLOListElement | undefined>()
  let takeAudio: HTMLAudioElement | undefined
  let runStream: ProgressSubscription | undefined
  let runStarting = $state(false)
  let runWatching = $state(false)
  let runEvent = $state<ProgressEvent | undefined>()
  let runCost = $state<number | undefined>()
  let runSentence = $state("")
  let lineNote = $state("")
  let boundaryRevision = $state(0)

  // The editor works on one language track. Every timing read and every write
  // uses the active track, so a two-language dub cannot show one track and save
  // another.
  let activeTrack = $derived(dub?.languages.find((track) => track.language === activeLanguage))
  let activeSegments = $derived(activeTrack?.segments ?? dub?.segments ?? [])
  let activeDub = $derived.by((): Dub | undefined => {
    const current = dub
    if (!current) return undefined
    return { ...current, segments: activeSegments }
  })
  let lineRows = $derived(activeDub ? fixtureLineRows(activeDub, activeLanguage) : [])
  let selectedRow = $derived(lineRows.find((row) => row.segment.id === selectedSegmentId))
  let selectedTake = $derived(selectedRow?.take)
  let sharedReferenceSlotMs = $derived(activeDub ? referenceSlotMs(activeDub) : 1)
  let sharedPictureDurationMs = $derived(activeDub ? pictureDurationMs(activeDub) : 0)
  let flaggedNotes = $derived(lineRows.filter((row) => row.line?.flagged).map(flagSentence))

  // A project the ledger holds no line or take for shows the waiting sentence
  // inside its workspace, because the run control must still render.
  let waitingForDubbing = $derived(
    !!dub
    && dub.segments.length === 0
    && dub.languages.every((track) => track.lines.length === 0)
  )
  let runStep = $derived.by((): ProcessingStep | undefined => {
    const event = runEvent
    if (!event) return undefined
    return {
      stage: event.stage,
      sentence: event.sentence,
      lineNumber: event.segment_id > 0 ? event.segment_id : undefined,
      language: event.language || undefined
    }
  })
  let railDub = $derived.by((): Dub | undefined => {
    if (!activeDub) return undefined
    return {
      ...activeDub,
      languages: activeDub.languages.filter((track) => track.language === activeLanguage)
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
    const current = activeDub
    if (!current?.segments.some((segment) => segment.id === segmentID)) return
    selectedSegmentId = segmentID
    lineNote = ""
    await tick()
    revealSelectedLengthRow()
    if (source !== "user") return
    const segment = current.segments.find((candidate) => candidate.id === segmentID)
    if (segment) preview?.seek(segment.start_ms / 1000)
  }

  function moveSelection(direction: "previous" | "next" | "left" | "right" | "up" | "down"): void {
    const segments = activeDub?.segments ?? []
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

  // The fixture bundles its takes as built asset URLs. A real project stores
  // an absolute server path, so the page names the take through the served
  // route and never needs the server's path.
  function takeAudioSource(take: Take): string {
    if (isFixtureID(projectID)) return fixtureTakeSource(take)
    return servedTakeURL(take)
  }

  function servedTakeURL(take: Take): string {
    const name = take.file.split(/[\\/]/).pop() ?? ""
    if (!name || !activeLanguage) return ""
    return `/api/dubs/${encodeURIComponent(projectID)}/takes/${encodeURIComponent(activeLanguage)}/${encodeURIComponent(name)}`
  }

  function playTake(take: Take): void {
    preview?.pause()
    stopTake()
    const source = takeAudioSource(take)
    if (!source) {
      lineNote = `Try ${take.attempt} on this line has no playable audio.`
      return
    }
    lineNote = ""
    const audio = new Audio(source)
    takeAudio = audio
    audio.addEventListener("ended", () => {
      if (takeAudio === audio) takeAudio = undefined
    }, { once: true })
    void audio.play().catch(() => undefined)
  }

  function handlePlaybackChange(snapshot: PlaybackSnapshot): void {
    playback = snapshot
    const current = activeDub
    if (snapshot.paused || !current) return
    const ms = snapshot.currentTime * 1000
    const hit = current.segments.find((segment) => ms >= segment.start_ms && ms < segment.end_ms)
    if (hit) selectSegment(hit.id, "playhead")
  }

  // The Boundary panel commits an overlap only after the creator confirms it,
  // because a collision opens the Keep overlap confirm instead of a save.
  // The route needs that confirmation as an explicit flag, because a silent
  // overlap must still fail.
  function overlapsAnotherLine(candidate: Segment, segments: readonly Segment[]): boolean {
    return segments.some((other) => (
      other.id !== candidate.id
      && candidate.start_ms < other.end_ms
      && candidate.end_ms > other.start_ms
    ))
  }

  async function handleBoundaryChange(change: BoundaryChange): Promise<void> {
    if (readOnly) return
    if (!dub) return
    commandPreview = undefined
    lineNote = ""
    const reply = await recordEdit({
      kind: "boundary",
      language: activeLanguage,
      segmentId: change.segment.id,
      startMs: change.segment.start_ms,
      endMs: change.segment.end_ms,
      allowOverlap: overlapsAnotherLine(change.segment, activeSegments)
    })
    if ("error" in reply) {
      // Remount Boundary from the unchanged payload, so a failed write
      // never leaves the editor showing a boundary that never saved.
      lineNote = reply.error
      boundaryRevision += 1
      return
    }
    applyEditedSegment(reply.segment)
  }

  // recordEdit sends one confirmed edit to the ledger write path. It returns
  // the saved segment, or an honest sentence when the write failed.
  async function recordEdit(request: EditRequest): Promise<{ segment: EditSegment } | { error: string }> {
    if (readOnly) return { error: fixtureReadOnlySentence }
    const body: Record<string, unknown> = { kind: request.kind, language: request.language }
    if (request.segmentId !== undefined) body.segment_id = request.segmentId
    if (request.startMs !== undefined) body.start_ms = request.startMs
    if (request.endMs !== undefined) body.end_ms = request.endMs
    if (request.command !== undefined) body.command = request.command
    if (request.durationMs !== undefined) body.duration_ms = request.durationMs
    if (request.allowOverlap) body.allow_overlap = true

    try {
      const response = await fetch(`/api/dubs/${encodeURIComponent(projectID)}/edits`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body)
      })
      if (!response.ok) return { error: await editFailureSentence(response) }
      const payload: unknown = await response.json().catch(() => undefined)
      if (!isEditPayload(payload)) {
        return { error: "The saved edit answer was incomplete. The timeline is unchanged." }
      }
      return { segment: payload.segment }
    } catch {
      return { error: "We could not reach the server, so the edit was not saved." }
    }
  }

  async function editFailureSentence(response: Response): Promise<string> {
    try {
      const body = (await response.json()) as { error?: unknown }
      if (typeof body.error === "string" && body.error.length > 0) return body.error
    } catch {
      // The failure body was not JSON, so the sentence below stands.
    }
    return `The edit was not saved (${response.status}).`
  }

  function isEditPayload(value: unknown): value is EditPayload {
    if (typeof value !== "object" || value === null) return false
    const payload = value as Partial<EditPayload>
    const segment = payload.segment
    if (typeof segment !== "object" || segment === null) return false
    return typeof payload.commit_id === "string"
      && typeof payload.sentence === "string"
      && typeof segment.id === "number"
      && typeof segment.start_ms === "number"
      && typeof segment.end_ms === "number"
      && typeof segment.duration_ms === "number"
      && typeof segment.speaker === "string"
  }

  // patchActiveSegment folds one change into the active language track, so the
  // editor shows the track it wrote and the change survives without a refetch.
  // The top-level segments mirror the first track, which is the source track.
  function patchActiveSegment(segmentID: number, patch: (segment: Segment) => Segment): void {
    const currentDub = dub
    if (!currentDub) return
    const tracks = currentDub.languages.map((track) => (
      track.language === activeLanguage
        ? {
            ...track,
            segments: track.segments.map((segment) => (
              segment.id === segmentID ? patch(segment) : segment
            ))
          }
        : track
    ))
    dub = {
      ...currentDub,
      segments: tracks[0]?.segments ?? currentDub.segments,
      languages: tracks
    }
  }

  function applyEditedSegment(segment: EditSegment): void {
    patchActiveSegment(segment.id, (candidate) => ({
      ...candidate,
      start_ms: segment.start_ms,
      end_ms: segment.end_ms,
      duration_ms: segment.duration_ms,
      speaker: segment.speaker
    }))
  }

  async function handleSpeakerChange(change: SpeakerChange): Promise<SpeakerChangeResult> {
    if (readOnly) return { error: fixtureReadOnlySentence }
    if (!dub) return { error: "This workspace is not ready for a re-render." }
    commandPreview = undefined
    lineNote = ""
    // A failed re-render must not leave the new speaker on screen. Boundary
    // reverts the same way, so the two controls agree.
    const stored = activeSegments.find((segment) => segment.id === change.segment.id)
    patchActiveSegment(change.segment.id, () => change.segment)
    const reply = await rerenderLine({
      segmentId: change.segment.id,
      language: activeLanguage,
      speaker: change.speaker
    })
    if ("error" in reply && stored) {
      patchActiveSegment(change.segment.id, () => stored)
    }
    if (selectedSegmentId === change.segment.id) {
      lineNote = "error" in reply ? reply.error : reply.sentence
    }
    return reply
  }

  async function rerenderLine(request: LineRerenderRequest): Promise<TextRerenderReply> {
    if (readOnly) return { error: fixtureReadOnlySentence }
    const currentDub = dub
    if (!currentDub) return { error: "This workspace is not ready for a re-render." }
    if (!request.language) return { error: "This project has no target language, so the line cannot re-render." }
    const body: Record<string, string> = { language: request.language }
    if (request.sourceText !== undefined) {
      body.source_text = request.sourceText
    } else if (request.text !== undefined) {
      body.text = request.text
    } else if (request.speaker === undefined) {
      return { error: "This line has no correction to send." }
    }
    if (request.speaker !== undefined) {
      body.speaker = request.speaker
    }

    try {
      const response = await fetch(
        `/api/dubs/${encodeURIComponent(projectID)}/lines/${request.segmentId}/rerender`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body)
        }
      )
      if (!response.ok) return { error: await rerenderFailureSentence(response) }
      const payload: unknown = await response.json().catch(() => undefined)
      if (!isRerenderPayload(payload)) {
        return { error: "The re-render answer was incomplete. The take is unchanged." }
      }
      applyRerenderedTake(payload, request)
      return {
        sentence: payload.sentence || `Line ${payload.segment_id} re-rendered as ${payload.take.name}.`,
        take: takeFromRerender(payload.take),
        totalNanodollars: payload.total_nanodollars,
        text: payload.text,
        sourceText: payload.source_text
      }
    } catch {
      return { error: "We could not reach the server, so the line was not re-rendered." }
    }
  }

  async function rerenderFailureSentence(response: Response): Promise<string> {
    try {
      const body = (await response.json()) as { error?: unknown }
      if (typeof body.error === "string" && body.error.length > 0) return body.error
    } catch {
      // The failure body was not JSON, so the sentence below stands.
    }
    return `The line did not re-render (${response.status}).`
  }

  function applyRerenderedTake(payload: RerenderPayload, request: LineRerenderRequest): void {
    const currentDub = dub
    if (!currentDub) return
    const take = takeFromRerender(payload.take)
    const correctedSource = request.sourceText
    const nextSpeaker = request.speaker
    const tracks = currentDub.languages.map((track) => {
      if (track.language !== request.language) return track
      return {
        ...track,
        segments: track.segments.map((segment) => {
          if (segment.id !== payload.segment_id) return segment
          if (correctedSource !== undefined) return { ...segment, text: correctedSource }
          if (nextSpeaker !== undefined) return { ...segment, speaker: nextSpeaker }
          return segment
        }),
        lines: track.lines.map((line) => (
          line.segment_id === payload.segment_id
            ? { ...line, text: payload.text, flagged: payload.take.flagged, takes: [...line.takes, take] }
            : line
        ))
      }
    })
    dub = {
      ...currentDub,
      segments: tracks[0]?.segments ?? currentDub.segments,
      languages: tracks
    }
  }

  function takeFromRerender(take: RerenderTakePayload): Take {
    return {
      file: take.file,
      voice: take.voice,
      attempt: take.attempt,
      repair: isRepairKind(take.repair) ? take.repair : "none",
      fit: take.fit,
      charges: take.charges
    }
  }

  function isRepairKind(value: string): value is RepairKind {
    return value === "none" || value === "atempo" || value === "rewrite" || value === "manual"
  }

  function isRerenderPayload(value: unknown): value is RerenderPayload {
    if (typeof value !== "object" || value === null) return false
    const payload = value as Partial<RerenderPayload>
    const take = payload.take
    if (typeof take !== "object" || take === null) return false
    return typeof payload.segment_id === "number"
      && typeof payload.sentence === "string"
      && typeof payload.total_nanodollars === "number"
      && typeof payload.text === "string"
      && typeof take.file === "string"
      && typeof take.name === "string"
      && typeof take.attempt === "number"
      && typeof take.voice === "string"
      && typeof take.repair === "string"
      && typeof take.flagged === "boolean"
      && typeof take.fit === "object"
      && take.fit !== null
      && Array.isArray(take.charges)
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

  async function previewCommand(command: string): Promise<CommandIntent | CommandParseFailure> {
    if (readOnly) return { error: fixtureReadOnlySentence }
    const current = activeDub
    if (!current) return { error: "This timeline is not ready for editing." }

    try {
      const response = await fetch("/api/editor/commands/preview", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          command,
          timeline: {
            duration_ms: pictureDurationMs(current),
            segments: current.segments.map(({ id, start_ms, end_ms, speaker }) => ({ id, start_ms, end_ms, speaker }))
          }
        })
      })
      if (!response.ok) {
        return {
          error: await commandPreviewError(response),
          canAskAgent: response.status === 422
        }
      }
      const result = await response.json() as CommandPreview
      if (!isCommandPreview(result, command)) return { error: "The command preview was incomplete. No timeline change was made." }
      commandPreview = result
      return { command, summary: result.summary }
    } catch {
      return { error: "We could not validate that command. No timeline change was made." }
    }
  }

  async function askEditorAgent(question: string): Promise<AgentTurn | CommandParseFailure> {
    if (readOnly) return { error: fixtureReadOnlySentence }
    if (!dub) return { error: "This timeline is not ready for the editor agent." }

    try {
      const response = await fetch(`/api/dubs/${encodeURIComponent(projectID)}/agent`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ question })
      })
      if (!response.ok) return { error: await agentFailureSentence(response) }
      const payload: unknown = await response.json().catch(() => undefined)
      if (!isAgentResponse(payload)) {
        return { error: "The editor agent answer was incomplete. No timeline change was made." }
      }
      return {
        answer: payload.answer,
        totalNanodollars: payload.total_nanodollars
      }
    } catch {
      return { error: "We could not reach the editor agent. No timeline change was made." }
    }
  }

  async function agentFailureSentence(response: Response): Promise<string> {
    try {
      const body = (await response.json()) as { error?: unknown }
      if (typeof body.error === "string" && body.error.length > 0) return body.error
    } catch {
      // The failure body was not JSON, so the sentence below stands.
    }
    return `The editor agent could not answer (${response.status}).`
  }

  function isAgentResponse(value: unknown): value is AgentResponse {
    if (typeof value !== "object" || value === null) return false
    const payload = value as Partial<AgentResponse>
    return typeof payload.answer === "string"
      && payload.answer.length > 0
      && Array.isArray(payload.charges)
      && typeof payload.total_nanodollars === "number"
      && Number.isSafeInteger(payload.total_nanodollars)
      && payload.total_nanodollars >= 0
  }

  async function confirmCommand(intent: CommandIntent): Promise<void> {
    if (readOnly) return
    const currentDub = dub
    const previewResult = commandPreview
    if (!currentDub || !previewResult || previewResult.command !== intent.command) {
      throw new Error("Please preview this command again before confirming it.")
    }
    lineNote = ""
    const reply = await recordEdit({
      kind: "command",
      language: activeLanguage,
      command: intent.command,
      durationMs: pictureDurationMs(currentDub)
    })
    if ("error" in reply) {
      lineNote = reply.error
      throw new Error(reply.error)
    }
    commandPreview = undefined
    applyEditedSegment(reply.segment)
    void selectSegment(reply.segment.id)
  }

  async function commandPreviewError(response: Response): Promise<string> {
    const message = (await response.text()).trim()
    return message || "We could not validate that command. No timeline change was made."
  }

  function isCommandPreview(value: unknown, command: string): value is CommandPreview {
    if (typeof value !== "object" || value === null) return false
    const preview = value as Partial<CommandPreview>
    return preview.command === command
      && typeof preview.summary === "string"
      && typeof preview.segment === "object"
      && preview.segment !== null
      && typeof preview.segment.id === "number"
      && typeof preview.segment.start_ms === "number"
      && typeof preview.segment.end_ms === "number"
      && typeof preview.segment.duration_ms === "number"
      && typeof preview.segment.speaker === "string"
  }

  function formatCost(nanodollars: number): string {
    return `$${(nanodollars / 1_000_000_000).toFixed(2)}`
  }

  function formatTakeLength(milliseconds: number): string {
    return `${(milliseconds / 1000).toFixed(2)}s`
  }

  function stopRunStream(): void {
    runStream?.close()
    runStream = undefined
  }

  function watchRun(): void {
    stopRunStream()
    runWatching = true
    runEvent = undefined
    runCost = undefined
    runSentence = ""
    runStream = watchRunProgress(projectID, {
      onEvent: (event) => {
        runCost = event.total_nanodollars
        if (event.type === "progress") {
          runEvent = event
          return
        }
        runWatching = false
        runEvent = undefined
        runSentence = event.sentence
      },
      onPhase: (update) => {
        if (update.phase === "live") {
          runSentence = ""
          return
        }
        if (update.phase === "reconnecting") {
          runSentence = update.sentence
          return
        }
        if (update.phase === "failed") {
          runWatching = false
          runEvent = undefined
          runSentence = update.sentence
        }
      }
    })
  }

  async function startFailureSentence(response: Response): Promise<string> {
    if (response.status === 503) return runUnavailableSentence
    if (response.status === 404) return runNoSourceSentence
    try {
      const body = (await response.json()) as { error?: unknown }
      if (typeof body.error === "string" && body.error.length > 0) return body.error
    } catch {
      // The failure body was not JSON, so the sentence below stands.
    }
    return runStartGenericSentence
  }

  async function startRun(): Promise<void> {
    if (runStarting || runWatching) return
    if (!activeLanguage) {
      runSentence = "This project has no target language, so the run cannot start."
      return
    }
    runStarting = true
    runSentence = ""
    try {
      const response = await fetch(
        `/api/dubs/${encodeURIComponent(projectID)}/run?language=${encodeURIComponent(activeLanguage)}`,
        { method: "POST" }
      )
      if (response.status === 409) {
        watchRun()
        return
      }
      if (!response.ok) {
        runSentence = await startFailureSentence(response)
        return
      }
      watchRun()
    } catch {
      runSentence = runStartFailedSentence
    } finally {
      runStarting = false
    }
  }

  function isWorkspaceDub(value: unknown): value is Dub {
    if (typeof value !== "object" || value === null) return false
    const candidate = value as Partial<Dub>
    return typeof candidate.id === "string"
      && typeof candidate.title === "string"
      && Array.isArray(candidate.segments)
      && Array.isArray(candidate.languages)
      && Array.isArray(candidate.charges)
      && Array.isArray(candidate.commits)
      && typeof candidate.total === "object"
      && candidate.total !== null
  }

  $effect(() => {
    const id = page.params.id
    if (!id || isFixtureID(id)) {
      if (page.url.searchParams.get("panel") !== "loading") {
        try {
          const loaded = loadFixtureDub(id ?? "fixture")
          dub = loaded
          workspaceState = loaded && loaded.segments.length > 0 && loaded.languages.length > 0
            ? { kind: "populated" }
            : {
                kind: "empty",
                sentence: pendingProjectSentence
              }
        } catch {
          dub = undefined
          workspaceState = {
            kind: "error",
            sentence: "We could not load this offline project, so no workspace data was changed."
          }
        }
      }
      return
    }

    dub = undefined
    workspaceState = { kind: "loading", sentence: "We are checking this project workspace." }

    let active = true
    fetch(`/api/dubs/${encodeURIComponent(id)}`)
      .then(async (response) => {
        if (!response.ok) return { ok: false as const, status: response.status }
        const payload: unknown = await response.json().catch(() => undefined)
        return { ok: true as const, payload }
      })
      .then((result) => {
        if (!active) return
        if (!result.ok) {
          dub = undefined
          workspaceState = {
            kind: "error",
            sentence: result.status === 404
              ? missingProjectSentence
              : result.status === 503
                ? ledgerUnavailableSentence
                : lookupFailedSentence
          }
          return
        }
        if (!isWorkspaceDub(result.payload)) {
          dub = undefined
          workspaceState = { kind: "error", sentence: lookupFailedSentence }
          return
        }
        const loaded = result.payload
        dub = loaded
        selectedSegmentId = loaded.segments[0]?.id ?? 0
        activeLanguage = loaded.languages[0]?.language ?? ""
        // A stored project with no take still renders its workspace. The
        // payload names its target language, so the run control has a
        // language to start into.
        workspaceState = loaded.languages.length > 0
          ? { kind: "populated" }
          : { kind: "empty", sentence: pendingProjectSentence }
      })
      .catch(() => {
        if (!active) return
        dub = undefined
        workspaceState = { kind: "error", sentence: lookupFailedSentence }
      })

    return () => {
      active = false
    }
  })

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

  onDestroy(() => {
    stopRunStream()
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
          {#if waitingForDubbing}
            <p class="waiting-note" role="status">{pendingProjectSentence}</p>
          {/if}
          <div class="run-panel">
            {#if !runWatching}
              <button type="button" class="run-start" onclick={startRun} disabled={runStarting}>
                {runStarting ? "Starting the dubbing run" : `Start the dubbing run into ${activeLanguage.toUpperCase()}`}
              </button>
            {/if}
            {#if runStep}
              <ProcessingBanner step={runStep} />
            {/if}
            {#if runCost !== undefined}
              <p class="run-cost">
                <span class="label">{runWatching ? "Running cost" : "Total cost"}</span>
                <span class="numeric">{formatCost(runCost)}</span>
              </p>
            {/if}
            {#if runSentence}
              <p class="run-note" role="status">{runSentence}</p>
            {/if}
          </div>
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
          <fieldset class="edit-control" disabled={readOnly} aria-label="Editor command" aria-describedby={readOnly ? "command-read-only" : undefined}>
            {#if readOnly}<p class="read-only-note" id="command-read-only">{fixtureReadOnlySentence}</p>{/if}
            <CommandBar bind:this={commandBar} parse={previewCommand} askagent={askEditorAgent} onconfirm={confirmCommand} />
          </fieldset>
          {#if selectedRow}
            <div class="editor-panels">
              <fieldset class="edit-control" disabled={readOnly} aria-label="Line boundary" aria-describedby={readOnly ? "boundary-read-only" : undefined}>
                {#if readOnly}<p class="read-only-note" id="boundary-read-only">{fixtureReadOnlySentence}</p>{/if}
                {#key boundaryRevision}
                  <Boundary
                    segment={selectedRow.segment}
                    segments={activeSegments}
                    takes={selectedRow.line?.takes ?? []}
                    timelineDurationMs={sharedPictureDurationMs}
                    onchange={handleBoundaryChange}
                  />
                {/key}
              </fieldset>
              <fieldset class="edit-control" disabled={readOnly} aria-label="Line speaker" aria-describedby={readOnly ? "speaker-read-only" : undefined}>
                {#if readOnly}<p class="read-only-note" id="speaker-read-only">{fixtureReadOnlySentence}</p>{/if}
                <Speaker
                  segment={selectedRow.segment}
                  segments={activeSegments}
                  takes={selectedRow.line?.takes ?? []}
                  onchange={handleSpeakerChange}
                />
              </fieldset>
              <fieldset class="edit-control text-control" disabled={readOnly} aria-label="Line text" aria-describedby={readOnly ? "text-read-only" : undefined}>
                {#if readOnly}<p class="read-only-note" id="text-read-only">{fixtureReadOnlySentence}</p>{/if}
                <Text
                  segment={selectedRow.segment}
                  line={selectedRow.line}
                  language={activeLanguage}
                  sourceLanguage={dub.source_language}
                  onrerender={rerenderLine}
                />
              </fieldset>
            </div>
          {/if}
          {#if selectedRow}
            {@const takes = selectedRow.line?.takes ?? []}
            <section
              class="take-history"
              aria-label={`Take history for line ${selectedRow.segment.id}`}
              data-selected-line={selectedRow.segment.id}
            >
              <p class="label">Take history</p>
              {#if takes.length > 0}
                <div class="take-list">
                  {#each takes as take, index (`${take.file}-${index}`)}
                    {@const isActive = index === takes.length - 1}
                    <button
                      type="button"
                      class:active={isActive}
                      class:ghost={!isActive}
                      class="take-chip"
                      data-take-attempt={take.attempt}
                      data-take-file={take.file}
                      data-take-state={isActive ? "active" : "ghost"}
                      aria-pressed={isActive}
                      onclick={() => playTake(take)}
                    >
                      Try {take.attempt} · {formatTakeLength(take.fit.measured_ms)}
                    </button>
                  {/each}
                </div>
              {:else}
                <p class="no-take">No take is ready for this line.</p>
              {/if}
              {#if lineNote}
                <p class="take-note" role="status">{lineNote}</p>
              {/if}
            </section>
          {/if}

          <Timeline
            segments={activeSegments}
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

  /* The editor row carries a command bar, three line panels, take history, and
     the timeline. An auto track let that stack grow past the workspace height
     and squeeze the picture and rail row to nothing. Capping the second track
     keeps both rows on screen, and the editor scrolls inside its own band. */
  .workspace {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 352px;
    grid-template-rows: minmax(220px, 1fr) minmax(156px, 46vh);
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

  .waiting-note {
    background: var(--raised);
    border: 1px solid var(--line-soft);
    border-radius: var(--radius-panel);
    color: var(--dim);
    font-size: 12.5px;
    line-height: 1.5;
    margin: 0 0 12px;
    padding: 12px;
  }

  .playback-note {
    color: var(--dim);
    font-size: 11.5px;
    margin: 8px 0 0;
  }

  .run-panel {
    display: grid;
    gap: 8px;
    margin-bottom: 12px;
  }

  .run-start {
    justify-self: start;
  }

  .run-cost {
    align-items: baseline;
    display: flex;
    gap: 8px;
    margin: 0;
  }

  .run-cost .label {
    margin: 0;
  }

  .run-note {
    color: var(--dim);
    font-size: 11.5px;
    margin: 0;
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

  .take-history {
    align-items: center;
    border-top: 1px solid var(--line-soft);
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 8px 16px 0;
  }

  .take-history .label {
    margin: 0;
  }

  .take-list {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .take-chip {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-pill);
    color: var(--text);
    font-size: 11px;
    padding: 3px 8px;
  }

  .take-chip.ghost {
    background: transparent;
    border-style: dashed;
    color: var(--dim);
    opacity: 0.72;
  }

  .take-chip.active {
    background: var(--fit);
    border-color: color-mix(in srgb, var(--fit), var(--text) 16%);
    font-weight: 650;
  }

  .take-note {
    color: var(--dim);
    font-size: 11.5px;
    margin: 0;
  }

  /* A column flex box lets the line panels absorb the shortfall. The command
     bar, take history, and timeline stop at their own minimums, so all three
     stay on screen. The timeline keeps its own 156px floor. */
  .editor {
    background: var(--surface);
    border-top: 1px solid var(--line);
    display: flex;
    flex-direction: column;
    grid-column: 1 / -1;
    min-height: 156px;
    overflow: auto;
  }

  .editor-panels {
    display: grid;
    gap: 12px;
    grid-template-columns: minmax(0, 1fr) 300px;
    min-height: 0;
    overflow: auto;
    padding: 12px 16px;
  }

  .edit-control {
    border: 0;
    margin: 0;
    min-width: 0;
    padding: 0;
  }

  .edit-control:disabled :global(button),
  .edit-control:disabled :global(input),
  .edit-control:disabled :global(textarea) {
    pointer-events: none;
  }

  .read-only-note {
    color: var(--dim);
    font-size: 11.5px;
    margin: 0;
    padding: 8px 12px;
  }

  .text-control {
    grid-column: 1 / -1;
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

    .editor-panels {
      grid-template-columns: 1fr;
    }
  }

</style>
