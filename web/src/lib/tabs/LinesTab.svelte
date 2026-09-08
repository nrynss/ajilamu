<script lang="ts">
  import type { Line, Segment, Take } from "$lib/types"

  type LineRow = {
    segment: Segment
    line?: Line
    take?: Take
  }

  interface Props {
    rows: readonly LineRow[]
    selectedSegmentId: number
    onselect?: (segmentID: number) => void
    onplaytake?: (take: Take) => void
  }

  let { rows, selectedSegmentId, onselect, onplaytake }: Props = $props()

  function formatDuration(milliseconds: number): string {
    return `${(milliseconds / 1000).toFixed(2)}s`
  }
</script>

<section class="lines" aria-label="Lines">
  <div class="column-headings" aria-hidden="true">
    <span>Line</span>
    <span>Slot</span>
    <span>Take</span>
    <span>Tries</span>
  </div>

  <ol>
    {#each rows as row (row.segment.id)}
      {@const selected = row.segment.id === selectedSegmentId}
      <li class:selected={selected}>
        <button
          class="line-number numeric"
          type="button"
          aria-current={selected ? "true" : undefined}
          aria-label={`Select line ${row.segment.id}`}
          onclick={() => onselect?.(row.segment.id)}
        >
          {row.segment.id}
        </button>
        {#if row.take}
          <button
            class="duration numeric"
            type="button"
            aria-label={`Play take for line ${row.segment.id} in its ${formatDuration(row.segment.duration_ms)} source slot`}
            onclick={() => onplaytake?.(row.take!)}
          >
            {formatDuration(row.segment.duration_ms)}
          </button>
        {:else}
          <span class="numeric" title="Source slot duration">{formatDuration(row.segment.duration_ms)}</span>
        {/if}
        {#if row.take}
          <button
            class="duration numeric"
            type="button"
            aria-label={`Play take for line ${row.segment.id}, ${formatDuration(row.take.fit.measured_ms)}`}
            onclick={() => onplaytake?.(row.take!)}
          >
            {formatDuration(row.take.fit.measured_ms)}
          </button>
        {:else}
          <span class="numeric missing">No take</span>
        {/if}
        <span class="numeric">{row.line?.takes.length ?? 0}</span>
      </li>
    {/each}
  </ol>
</section>

<style>
  .lines {
    min-width: 0;
  }

  .column-headings,
  li {
    align-items: center;
    display: grid;
    gap: 8px;
    grid-template-columns: 42px minmax(48px, 1fr) minmax(48px, 1fr) 34px;
  }

  .column-headings {
    border-bottom: 1px solid var(--line-soft);
    color: var(--faint);
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.06em;
    padding: 0 14px 7px;
    text-transform: uppercase;
  }

  ol {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  li {
    border-bottom: 1px solid var(--line-soft);
    color: var(--dim);
    font-size: 11.5px;
    min-height: 38px;
    padding: 5px 14px;
  }

  li.selected {
    background: var(--accent-q);
    color: var(--text);
  }

  .line-number,
  .duration {
    background: transparent;
    border-color: transparent;
    border-radius: var(--radius-control);
    color: inherit;
    font-size: inherit;
    padding: 3px 4px;
    text-align: left;
  }

  .line-number {
    color: var(--text);
    font-weight: 700;
    text-align: center;
  }

  .duration:hover,
  .duration:focus-visible,
  .line-number:focus-visible {
    border-color: var(--accent);
    outline: none;
  }

  .duration {
    text-decoration: underline;
    text-decoration-color: var(--line);
    text-underline-offset: 3px;
  }

  .missing {
    color: var(--faint);
  }
</style>
