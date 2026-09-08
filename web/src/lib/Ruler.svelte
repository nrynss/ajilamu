<script lang="ts">
  type RulerProps = {
    durationMs: number
    pixelsPerSecond?: number
  }

  let { durationMs, pixelsPerSecond = 24 }: RulerProps = $props()

  let width = $derived(Math.max(0, (durationMs / 1000) * pixelsPerSecond))
  let ticks = $derived.by(() => {
    const intervalMs = durationMs > 60_000 ? 10_000 : 5_000
    const nextTicks: number[] = []

    for (let time = 0; time <= durationMs; time += intervalMs) {
      nextTicks.push(time)
    }

    if (nextTicks.at(-1) !== durationMs) {
      nextTicks.push(durationMs)
    }

    return nextTicks
  })

  function timecode(milliseconds: number) {
    const totalSeconds = milliseconds / 1000
    const minutes = Math.floor(totalSeconds / 60)
    const seconds = totalSeconds - minutes * 60

    return `${minutes}:${seconds.toFixed(milliseconds % 1000 === 0 ? 0 : 1).padStart(2, "0")}`
  }
</script>

<div
  class="ruler"
  data-duration-ms={durationMs}
  style={`width: ${width}px`}
  aria-label="Time ruler ending at {timecode(durationMs)}"
>
  {#each ticks as tick}
    <span
      class:terminal-tick={tick === durationMs}
      class="tick numeric"
      data-time-ms={tick}
      style={`left: ${(tick / 1000) * pixelsPerSecond}px`}
    >
      <i aria-hidden="true"></i>
      <b>{timecode(tick)}</b>
    </span>
  {/each}
</div>

<style>
  .ruler {
    height: 30px;
    position: relative;
  }

  .tick {
    color: var(--faint);
    font-size: 10px;
    font-style: normal;
    height: 30px;
    position: absolute;
    top: 0;
    transform: translateX(-0.5px);
    white-space: nowrap;
  }

  .tick i {
    background: var(--line);
    display: block;
    height: 7px;
    width: 1px;
  }

  .tick b {
    display: block;
    font-weight: 500;
    margin-top: 2px;
    transform: translateX(-2px);
  }

  .terminal-tick b {
    transform: translateX(-100%);
  }
</style>
