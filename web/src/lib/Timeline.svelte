<script lang="ts">
  import Ruler from "./Ruler.svelte"
  import Track from "./Track.svelte"
  import type { LanguageTrack, Segment } from "./types"

  type TimelineProps = {
    segments: Segment[]
    tracks: LanguageTrack[]
    durationMs?: number
  }

  const minimumHeight = 156
  const maximumHeight = 640
  const pixelsPerSecond = 24

  let {
    segments,
    tracks,
    durationMs
  }: TimelineProps = $props()

  let timelineHeight = $state(256)
  let resizing = $state(false)
  let resizeStartY = 0
  let resizeStartHeight = 0
  let renderedDurationMs = $derived(durationMs ?? Math.max(0, ...segments.map((segment) => segment.end_ms)))
  let scaleWidth = $derived(Math.max(640, (renderedDurationMs / 1000) * pixelsPerSecond))

  function formatDuration(milliseconds: number) {
    const totalSeconds = milliseconds / 1000
    const minutes = Math.floor(totalSeconds / 60)
    const seconds = totalSeconds - minutes * 60

    return `${minutes}:${seconds.toFixed(1).padStart(4, "0")}`
  }

  function constrainHeight(height: number) {
    return Math.min(maximumHeight, Math.max(minimumHeight, height))
  }

  function beginResize(event: PointerEvent) {
    event.preventDefault()
    resizeStartY = event.clientY
    resizeStartHeight = timelineHeight
    resizing = true
  }

  function resize(event: PointerEvent) {
    if (!resizing) {
      return
    }

    timelineHeight = constrainHeight(resizeStartHeight + resizeStartY - event.clientY)
  }

  function endResize() {
    resizing = false
  }

  function resizeWithKeyboard(event: KeyboardEvent) {
    if (event.key === "ArrowUp") {
      timelineHeight = constrainHeight(timelineHeight + 16)
      event.preventDefault()
    }

    if (event.key === "ArrowDown") {
      timelineHeight = constrainHeight(timelineHeight - 16)
      event.preventDefault()
    }
  }

  function trackLabel(language: string) {
    return `Dub ${language.toUpperCase()}`
  }
</script>

<svelte:window onpointermove={resize} onpointerup={endResize} onpointercancel={endResize} />

<section
  class:resizing
  class="timeline"
  data-duration-ms={renderedDurationMs}
  data-minimum-height={minimumHeight}
  style={`height: ${timelineHeight}px`}
  aria-label="Dubbing timeline"
>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
  <div
    aria-label="Resize timeline. Use the up and down arrow keys for small changes."
    aria-orientation="horizontal"
    aria-valuemax={maximumHeight}
    aria-valuemin={minimumHeight}
    aria-valuenow={timelineHeight}
    aria-valuetext={`Timeline height ${timelineHeight} pixels. Minimum ${minimumHeight} pixels. Maximum ${maximumHeight} pixels.`}
    class="resize-grip"
    onkeydown={resizeWithKeyboard}
    onpointerdown={beginResize}
    role="separator"
    tabindex="0"
  >
    <span aria-hidden="true"></span>
  </div>

  <div class="timeline-scroll">
    <div class="timeline-canvas" style={`width: ${116 + scaleWidth}px`}>
      <header class="timeline-header">
        <div class="header-label">Timeline</div>
        <div class="header-details">
          <span>Original audio and dubbed takes</span>
          <span class="numeric">{formatDuration(renderedDurationMs)}</span>
        </div>
      </header>

      <div class="ruler-row">
        <div class="ruler-label">Time</div>
        <Ruler durationMs={renderedDurationMs} {pixelsPerSecond} />
      </div>

      <div class="tracks">
        <Track label="Original" {segments} durationMs={renderedDurationMs} {pixelsPerSecond} source />
        {#each tracks as track (track.language)}
          <Track
            label={trackLabel(track.language)}
            {segments}
            durationMs={renderedDurationMs}
            lines={track.lines}
            {pixelsPerSecond}
          />
        {/each}
      </div>
    </div>
  </div>
</section>

<style>
  .timeline {
    background: var(--surface);
    border-top: 1px solid var(--line);
    min-height: 156px;
    position: relative;
  }

  .timeline.resizing {
    cursor: row-resize;
    user-select: none;
  }

  .resize-grip {
    align-items: center;
    background: transparent;
    border: 0;
    cursor: row-resize;
    display: flex;
    height: 12px;
    inset: -6px 0 auto;
    justify-content: center;
    padding: 0;
    position: absolute;
    width: 100%;
    z-index: 8;
  }

  .resize-grip span {
    background: var(--line);
    border-radius: var(--radius-pill);
    height: 2px;
    width: 34px;
  }

  .resize-grip:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .timeline-scroll {
    height: 100%;
    overflow: auto;
  }

  .timeline-header,
  .ruler-row {
    display: grid;
    grid-template-columns: 116px max-content;
  }

  .timeline-header {
    background: var(--surface);
    border-bottom: 1px solid var(--line);
    height: 44px;
    position: sticky;
    top: 0;
    z-index: 6;
  }

  .header-label,
  .ruler-label {
    align-items: center;
    background: var(--surface);
    border-right: 1px solid var(--line-soft);
    color: var(--dim);
    display: flex;
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.08em;
    padding: 0 16px;
    position: sticky;
    left: 0;
    text-transform: uppercase;
    z-index: 7;
  }

  .header-details {
    align-items: center;
    color: var(--dim);
    display: flex;
    font-size: 11.5px;
    justify-content: space-between;
    padding: 0 16px;
  }

  .header-details .numeric {
    color: var(--faint);
  }

  .ruler-row {
    background: var(--surface);
    border-bottom: 1px solid var(--line-soft);
    height: 31px;
    position: sticky;
    top: 44px;
    z-index: 5;
  }

  .ruler-label {
    color: var(--faint);
  }

  .tracks {
    min-height: 64px;
  }

  @media (max-width: 560px) {
    .header-label,
    .ruler-label {
      padding: 0 10px;
    }

    .timeline-canvas {
      min-width: 728px;
    }
  }
</style>
