<script lang="ts">
  import LengthBar from "$lib/LengthBar.svelte"
  import type { Fit, FitState, Segment, Take } from "$lib/types"

  export type BoundaryHandle = "start" | "end"

  export interface BoundaryChange {
    segment: Segment
    handle: BoundaryHandle
    snappedToMs?: number
  }

  export interface BoundaryCollision {
    candidate: Segment
    collidingSegments: Segment[]
    handle: BoundaryHandle
  }

  interface Props {
    /** The timeline segment that the editor changes. */
    segment: Segment
    /** Every source segment supplies snap targets and collision checks. */
    segments: readonly Segment[]
    /** The active and older takes draw against the changing slot. */
    takes?: readonly Take[]
    /** The source picture limits dragging past the last frame. */
    timelineDurationMs?: number
    /** This keeps the boundary ruler aligned with Timeline.svelte by default. */
    pixelsPerSecond?: number
    /** A nearby source boundary becomes an exact edit target. */
    snapThresholdMs?: number
    /** A slot cannot shrink below this duration. */
    minimumDurationMs?: number
    /** Keyboard arrows move a handle by this many milliseconds. */
    keyboardStepMs?: number
    /** The parent persists a confirmed change. */
    onchange?: (change: BoundaryChange) => void
    /** The parent can record a blocked overlap without accepting it. */
    oncollision?: (collision: BoundaryCollision) => void
  }

  let {
    segment,
    segments,
    takes = [],
    timelineDurationMs,
    pixelsPerSecond = 24,
    snapThresholdMs = 120,
    minimumDurationMs = 100,
    keyboardStepMs = 50,
    onchange,
    oncollision
  }: Props = $props()

  // svelte-ignore state_referenced_locally
  let editedSegment = $state<Segment>(copySegment(segment))
  // svelte-ignore state_referenced_locally
  let committedSegment = $state<Segment>(copySegment(segment))
  let activeHandle = $state<BoundaryHandle | undefined>()
  let pendingCollision = $state<BoundaryCollision | undefined>()
  let track = $state<HTMLDivElement | undefined>()
  let sourceSignature = $state("")

  const timelineEndMs = $derived(
    Math.max(timelineDurationMs ?? 0, ...segments.map((candidate) => candidate.end_ms), editedSegment.end_ms)
  )
  const laneWidth = $derived(Math.max(1, (timelineEndMs / 1000) * pixelsPerSecond))
  const snapPoints = $derived.by(() => {
    const points = new Set<number>([0, timelineEndMs])
    for (const candidate of segments) {
      if (candidate.id === editedSegment.id) continue
      points.add(candidate.start_ms)
      points.add(candidate.end_ms)
    }
    return [...points].sort((left, right) => left - right)
  })
  const silenceGaps = $derived.by(() => {
    const ordered = [...segments].sort((left, right) => left.start_ms - right.start_ms)
    const gaps: Array<{ start: number; end: number }> = []
    for (let index = 1; index < ordered.length; index += 1) {
      const previous = ordered[index - 1]
      const next = ordered[index]
      if (next.start_ms > previous.end_ms) gaps.push({ start: previous.end_ms, end: next.start_ms })
    }
    return gaps
  })
  const adjustedTakes = $derived(takes.map((take) => ({
    ...take,
    fit: fitFor(take.fit.measured_ms, editedSegment.duration_ms)
  })))
  const slotStyle = $derived(
    `--slot-left: ${(editedSegment.start_ms / 1000) * pixelsPerSecond}px; --slot-width: ${(editedSegment.duration_ms / 1000) * pixelsPerSecond}px`
  )

  $effect(() => {
    const nextSignature = `${segment.id}:${segment.start_ms}:${segment.end_ms}:${segment.duration_ms}`
    if (nextSignature === sourceSignature || activeHandle || pendingCollision) return
    sourceSignature = nextSignature
    editedSegment = copySegment(segment)
    committedSegment = copySegment(segment)
  })

  function copySegment(value: Segment): Segment {
    return { ...value }
  }

  function fitFor(measuredMs: number, slotMs: number): Fit {
    const delta_ms = measuredMs - slotMs
    const state: FitState = delta_ms > 0 ? "too_long" : delta_ms < 0 ? "too_short" : "fits"
    return { slot_ms: slotMs, measured_ms: measuredMs, delta_ms, state }
  }

  function constrain(handle: BoundaryHandle, value: number): number {
    if (handle === "start") {
      return Math.min(editedSegment.end_ms - minimumDurationMs, Math.max(0, value))
    }
    return Math.max(editedSegment.start_ms + minimumDurationMs, Math.min(timelineEndMs, value))
  }

  function snap(value: number): { value: number; snappedToMs?: number } {
    const closest = snapPoints.reduce<number | undefined>((best, point) => {
      if (Math.abs(point - value) > snapThresholdMs) return best
      if (best === undefined || Math.abs(point - value) < Math.abs(best - value)) return point
      return best
    }, undefined)
    return closest === undefined ? { value } : { value: closest, snappedToMs: closest }
  }

  function candidateFor(
    handle: BoundaryHandle,
    value: number,
    shouldSnap = true
  ): { segment: Segment; snappedToMs?: number } {
    const constrained = constrain(handle, Math.round(value))
    const snapped = shouldSnap ? snap(constrained) : { value: constrained }
    const constrainedValue = constrain(handle, snapped.value)
    const candidate = copySegment(editedSegment)
    if (handle === "start") candidate.start_ms = constrainedValue
    else candidate.end_ms = constrainedValue
    candidate.duration_ms = candidate.end_ms - candidate.start_ms
    return {
      segment: candidate,
      snappedToMs: constrainedValue === snapped.value ? snapped.snappedToMs : undefined
    }
  }

  function collisions(candidate: Segment): Segment[] {
    return segments.filter((other) => (
      other.id !== candidate.id
      && candidate.start_ms < other.end_ms
      && candidate.end_ms > other.start_ms
    ))
  }

  function propose(handle: BoundaryHandle, value: number, finish = false, shouldSnap = true): void {
    const next = candidateFor(handle, value, shouldSnap)
    editedSegment = next.segment
    if (!finish) return

    activeHandle = undefined
    const blockedBy = collisions(next.segment)
    if (blockedBy.length > 0) {
      const collision = { candidate: next.segment, collidingSegments: blockedBy, handle }
      pendingCollision = collision
      oncollision?.(collision)
      return
    }
    commit(next.segment, handle, next.snappedToMs)
  }

  function commit(next: Segment, handle: BoundaryHandle, snappedToMs?: number): void {
    editedSegment = next
    committedSegment = copySegment(next)
    pendingCollision = undefined
    onchange?.({ segment: next, handle, snappedToMs })
  }

  function beginDrag(event: PointerEvent, handle: BoundaryHandle): void {
    event.preventDefault()
    pendingCollision = undefined
    activeHandle = handle
    const target = event.currentTarget
    if (target instanceof HTMLElement) target.setPointerCapture(event.pointerId)
    updateFromPointer(event, false)
  }

  function updateFromPointer(event: PointerEvent, finish: boolean): void {
    if (!activeHandle || !track) return
    const bounds = track.getBoundingClientRect()
    const value = ((event.clientX - bounds.left) / pixelsPerSecond) * 1000
    propose(activeHandle, value, finish)
  }

  function moveDrag(event: PointerEvent): void {
    if (activeHandle) updateFromPointer(event, false)
  }

  function finishDrag(event: PointerEvent): void {
    if (activeHandle) updateFromPointer(event, true)
  }

  function cancelDrag(): void {
    activeHandle = undefined
  }

  function moveWithKeyboard(event: KeyboardEvent, handle: BoundaryHandle): void {
    const direction = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0
    if (direction === 0) return
    event.preventDefault()
    const multiplier = event.shiftKey ? 10 : 1
    const current = handle === "start" ? editedSegment.start_ms : editedSegment.end_ms
    propose(handle, current + direction * keyboardStepMs * multiplier, true, false)
  }

  function keyboardHandle(node: HTMLButtonElement, handle: BoundaryHandle): { destroy: () => void } {
    const listener = (event: KeyboardEvent) => moveWithKeyboard(event, handle)
    node.addEventListener("keydown", listener)
    return { destroy: () => node.removeEventListener("keydown", listener) }
  }

  function confirmOverlap(): void {
    const collision = pendingCollision
    if (!collision) return
    commit(collision.candidate, collision.handle)
  }

  function cancelOverlap(): void {
    pendingCollision = undefined
    editedSegment = copySegment(committedSegment)
  }

  function timeText(milliseconds: number): string {
    const seconds = milliseconds / 1000
    const minutes = Math.floor(seconds / 60)
    return `${minutes}:${(seconds - minutes * 60).toFixed(3).padStart(6, "0")}`
  }
</script>

<svelte:window onpointermove={moveDrag} onpointerup={finishDrag} onpointercancel={cancelDrag} />

<section class="boundary-editor" aria-label={`Adjust boundary for line ${editedSegment.id}`}>
  <div class="editor-heading">
    <div>
      <p class="eyebrow">Line {editedSegment.id}</p>
      <h2>Adjust timing</h2>
    </div>
    <p class="numeric">{timeText(editedSegment.start_ms)} to {timeText(editedSegment.end_ms)}</p>
  </div>

  <div class="boundary-scroll">
    <div
      bind:this={track}
      class:dragging={activeHandle !== undefined}
      class="boundary-track"
      data-duration-ms={editedSegment.duration_ms}
      data-segment-id={editedSegment.id}
      style={`width: ${laneWidth}px`}
    >
      {#each silenceGaps as gap (gap.start)}
        <span
          aria-hidden="true"
          class="silence-gap"
          data-silence-end-ms={gap.end}
          data-silence-start-ms={gap.start}
          style={`left: ${(gap.start / 1000) * pixelsPerSecond}px; width: ${((gap.end - gap.start) / 1000) * pixelsPerSecond}px`}
        ></span>
      {/each}

      <div class="slot" style={slotStyle}>
        <span class="slot-label">Line {editedSegment.id}</span>
      </div>

      <button
        aria-label={`Move start of line ${editedSegment.id}`}
        aria-valuemax={editedSegment.end_ms - minimumDurationMs}
        aria-valuemin={0}
        aria-valuenow={editedSegment.start_ms}
        aria-valuetext={`Start ${timeText(editedSegment.start_ms)}`}
        class="boundary-handle start"
        data-handle="start"
        onpointerdown={(event) => beginDrag(event, "start")}
        role="slider"
        style={`left: ${(editedSegment.start_ms / 1000) * pixelsPerSecond}px`}
        type="button"
        use:keyboardHandle={"start"}
      ><span aria-hidden="true"></span></button>

      <button
        aria-label={`Move end of line ${editedSegment.id}`}
        aria-valuemax={timelineEndMs}
        aria-valuemin={editedSegment.start_ms + minimumDurationMs}
        aria-valuenow={editedSegment.end_ms}
        aria-valuetext={`End ${timeText(editedSegment.end_ms)}`}
        class="boundary-handle end"
        data-handle="end"
        onpointerdown={(event) => beginDrag(event, "end")}
        role="slider"
        style={`left: ${(editedSegment.end_ms / 1000) * pixelsPerSecond}px`}
        type="button"
        use:keyboardHandle={"end"}
      ><span aria-hidden="true"></span></button>
    </div>
  </div>

  <p class="hint">Drag a handle to a nearby line edge or a silent gap. Arrow keys nudge by {keyboardStepMs} milliseconds. Shift moves ten steps.</p>

  <div class="length-bar">
    <LengthBar
      ariaLabel={`Line ${editedSegment.id} length bar for a ${timeText(editedSegment.duration_ms)} slot.`}
      segmentId={editedSegment.id}
      slotMs={editedSegment.duration_ms}
      takes={adjustedTakes}
    />
  </div>

  {#if pendingCollision}
    <aside aria-live="assertive" class="collision" role="alert">
      <p>This timing overlaps line{pendingCollision.collidingSegments.length === 1 ? "" : "s"} {pendingCollision.collidingSegments.map((other) => other.id).join(", ")}. Keep the overlap?</p>
      <div>
        <button onclick={cancelOverlap} type="button">Keep lines separate</button>
        <button class="confirm" onclick={confirmOverlap} type="button">Keep overlap</button>
      </div>
    </aside>
  {/if}
</section>

<style>
  .boundary-editor {
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

  .boundary-scroll {
    margin: 16px -16px 0;
    overflow-x: auto;
    padding: 0 16px 8px;
  }

  .boundary-track {
    background: var(--sunken);
    border: 1px solid var(--line-soft);
    border-radius: 4px;
    height: 58px;
    min-width: 100%;
    position: relative;
  }

  .boundary-track.dragging {
    cursor: col-resize;
    user-select: none;
  }

  .silence-gap {
    background: repeating-linear-gradient(135deg, transparent 0 4px, color-mix(in srgb, var(--short), transparent 65%) 4px 5px);
    bottom: 0;
    position: absolute;
    top: 0;
  }

  .slot {
    background: color-mix(in srgb, var(--fit), transparent 74%);
    border: 1px solid var(--fit);
    border-radius: 3px;
    bottom: 8px;
    left: var(--slot-left);
    position: absolute;
    top: 8px;
    width: var(--slot-width);
  }

  .slot-label {
    color: var(--text);
    font-size: 10px;
    left: 7px;
    position: absolute;
    top: 5px;
    white-space: nowrap;
  }

  .boundary-handle {
    background: var(--surface);
    border: 2px solid var(--accent);
    border-radius: 50%;
    cursor: col-resize;
    height: 18px;
    margin-left: -9px;
    padding: 0;
    position: absolute;
    top: 20px;
    width: 18px;
    z-index: 2;
  }

  .boundary-handle span {
    border-left: 1px solid var(--accent);
    display: block;
    height: 8px;
    margin: auto;
    width: 0;
  }

  .boundary-handle:focus-visible {
    outline: 2px solid var(--text);
    outline-offset: 3px;
  }

  .hint {
    color: var(--dim);
    font-size: 11px;
    line-height: 1.45;
    margin: 0 0 12px;
  }

  .length-bar {
    border-top: 1px solid var(--line-soft);
    margin: 0 -8px;
    padding: 8px 8px 0;
  }

  .collision {
    background: color-mix(in srgb, var(--over), transparent 85%);
    border: 1px solid var(--over);
    border-radius: 4px;
    margin-top: 12px;
    padding: 10px;
  }

  .collision p {
    font-size: 12px;
    margin: 0 0 8px;
  }

  .collision div {
    display: flex;
    gap: 8px;
  }

  .collision button {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 3px;
    color: var(--text);
    cursor: pointer;
    font: inherit;
    font-size: 11px;
    padding: 5px 8px;
  }

  .collision .confirm {
    background: var(--over);
    border-color: var(--over);
  }
</style>
