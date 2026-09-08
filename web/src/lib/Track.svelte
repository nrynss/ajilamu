<script lang="ts">
  import type { Line, Segment } from "./types"

  type TrackProps = {
    label: string
    segments: Segment[]
    lines?: Line[]
    durationMs?: number
    pixelsPerSecond?: number
    source?: boolean
  }

  let {
    label,
    segments,
    lines = [],
    durationMs,
    pixelsPerSecond = 24,
    source = false
  }: TrackProps = $props()

  let linesBySegment = $derived.by(() => new Map(lines.map((line) => [line.segment_id, line])))
  let laneWidth = $derived(
    ((durationMs ?? Math.max(0, ...segments.map((segment) => segment.end_ms))) / 1000) * pixelsPerSecond
  )

  function slotStyle(segment: Segment) {
    return `--slot-left: ${(segment.start_ms / 1000) * pixelsPerSecond}px; --slot-width: ${(segment.duration_ms / 1000) * pixelsPerSecond}px`
  }

  function audioStyle(segment: Segment, measuredMs: number) {
    return `${slotStyle(segment)}; --audio-width: ${(measuredMs / 1000) * pixelsPerSecond}px`
  }

  function peakX(index: number, length: number) {
    return ((index + 0.5) / length) * 100
  }

  function peakTop(peak: number) {
    return 50 - (peak / 255) * 38
  }

  function peakBottom(peak: number) {
    return 50 + (peak / 255) * 38
  }
</script>

<section class:source class="track" aria-label={label} data-track={label}>
  <div class="track-label">
    <span>{label}</span>
    {#if !source}
      <small>Dub</small>
    {/if}
  </div>

  <div class="track-lane" aria-label="{label} audio segments" style={`width: ${laneWidth}px`}>
    {#each segments as segment (segment.id)}
      {@const line = linesBySegment.get(segment.id)}
      {@const take = line?.takes.at(-1)}
      <div
        class:flagged={line?.flagged}
        class="slot"
        data-duration-ms={segment.duration_ms}
        data-segment-id={segment.id}
        data-start-ms={segment.start_ms}
        style={slotStyle(segment)}
      >
        {#if source}
          <span class="source-number numeric">{String(segment.id).padStart(2, "0")}</span>
        {/if}
      </div>

      {#if take}
        <div
          class:overrun={take.fit.delta_ms > 0}
          class="take"
          data-duration-ms={take.fit.measured_ms}
          data-file={take.file}
          data-fit-state={take.fit.state}
          data-segment-id={segment.id}
          style={audioStyle(segment, take.fit.measured_ms)}
          title="Line {segment.id}, {take.fit.measured_ms} ms"
        >
          {#if take.peaks?.length}
            <svg aria-hidden="true" class="waveform" preserveAspectRatio="none" viewBox="0 0 100 100">
              {#each take.peaks as peak, index}
                <line
                  x1={peakX(index, take.peaks.length)}
                  x2={peakX(index, take.peaks.length)}
                  y1={peakTop(peak)}
                  y2={peakBottom(peak)}
                ></line>
              {/each}
            </svg>
          {/if}
          <span class="take-number numeric">{String(segment.id).padStart(2, "0")}</span>
        </div>
      {/if}

      {#if take && line?.flagged && take.fit.delta_ms < 0}
        <span
          class="short-tail"
          aria-label="Line {segment.id} has {Math.abs(take.fit.delta_ms)} milliseconds without dubbed audio"
          style={`${slotStyle(segment)}; --audio-width: ${(take.fit.measured_ms / 1000) * pixelsPerSecond}px`}
        ></span>
      {/if}
    {/each}
  </div>
</section>

<style>
  .track {
    display: grid;
    grid-template-columns: 116px max-content;
    min-height: 64px;
  }

  .track-label {
    align-items: center;
    background: var(--surface);
    border-bottom: 1px solid var(--line-soft);
    color: var(--dim);
    display: flex;
    font-size: 11.5px;
    gap: 6px;
    padding: 0 12px 0 16px;
    position: sticky;
    left: 0;
    z-index: 3;
  }

  .track-label small {
    color: var(--faint);
    font-size: 10px;
  }

  .track-lane {
    background: var(--surface);
    border-bottom: 1px solid var(--line-soft);
    height: 64px;
    position: relative;
  }

  .slot,
  .take,
  .short-tail {
    left: var(--slot-left);
    position: absolute;
  }

  .slot {
    background: var(--sunken);
    border: 1px solid var(--line);
    border-radius: 3px;
    height: 42px;
    top: 11px;
    width: var(--slot-width);
  }

  .source .slot {
    background: var(--raised);
  }

  .source-number {
    color: var(--faint);
    display: block;
    font-size: 10px;
    padding: 3px 5px;
  }

  .slot.flagged {
    border-color: var(--short);
  }

  .take {
    background: var(--fit);
    border: 1px solid color-mix(in srgb, var(--fit), var(--text) 16%);
    border-radius: 3px;
    height: 32px;
    overflow: hidden;
    top: 16px;
    width: var(--audio-width);
    z-index: 2;
  }

  .take.overrun {
    background: var(--over);
    border-color: color-mix(in srgb, var(--over), var(--text) 16%);
  }

  .waveform {
    height: 100%;
    inset: 0;
    position: absolute;
    width: 100%;
  }

  .waveform line {
    stroke: color-mix(in srgb, var(--surface), transparent 28%);
    stroke-width: 0.8;
  }

  .take-number {
    bottom: 3px;
    color: color-mix(in srgb, var(--text), var(--surface) 45%);
    font-size: 9px;
    left: 5px;
    position: absolute;
  }

  .short-tail {
    background: color-mix(in srgb, var(--short), transparent 24%);
    border-radius: 0 2px 2px 0;
    height: 30px;
    left: calc(var(--slot-left) + var(--audio-width));
    top: 17px;
    width: calc(var(--slot-width) - var(--audio-width));
    z-index: 1;
  }
</style>
