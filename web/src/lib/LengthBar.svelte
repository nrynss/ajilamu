<script lang="ts">
  import { onMount } from "svelte"
  import type { Take } from "$lib/types"

  export interface LengthBarProps {
    /** The dialogue slot duration, in milliseconds. */
    slotMs: number
    /** Attempts ordered oldest first. The last one is the active take. */
    takes: Take[]
    /** The largest slot in the surrounding list. It preserves true scale between bars. */
    referenceSlotMs?: number
    /** The segment number makes the canvas description specific for assistive technology. */
    segmentId?: number
    /** Changes when the parent switches themes. Root token changes are also observed. */
    theme?: string
    /** Replaces the generated accessible description when supplied. */
    ariaLabel?: string
  }

  let {
    slotMs,
    takes,
    referenceSlotMs,
    segmentId,
    theme,
    ariaLabel
  }: LengthBarProps = $props()

  let canvas: HTMLCanvasElement
  let frame = 0
  let paintWidth = $state(0)

  const canvasHeight = 48
  const horizontalInset = 8
  const troughY = 8
  const troughHeight = 20
  const cornerRadius = 4

  const activeTake = $derived(takes.at(-1))
  const scaleCeilingMs = $derived(Math.max(referenceSlotMs ?? slotMs, slotMs))
  const accessibleDescription = $derived(
    ariaLabel ?? describeTake(segmentId, activeTake, slotMs)
  )

  function durationText(durationMs: number): string {
    return `${(Math.abs(durationMs) / 1000).toFixed(2)} seconds`
  }

  function describeTake(segment: number | undefined, take: Take | undefined, slot: number): string {
    const prefix = segment === undefined ? "Length bar" : `Line ${segment} length bar`

    if (!take) {
      return `${prefix}. No take has been recorded for this ${durationText(slot)} slot.`
    }

    if (take.fit.state === "too_short") {
      return `${prefix}. This take is ${durationText(take.fit.delta_ms)} short of the slot.`
    }

    if (take.fit.state === "too_long") {
      return `${prefix}. This take overruns the slot by ${durationText(take.fit.delta_ms)}.`
    }

    return `${prefix}. This take fits the ${durationText(slot)} slot.`
  }

  function token(name: string): string {
    return getComputedStyle(canvas).getPropertyValue(name).trim()
  }

  function roundedRect(context: CanvasRenderingContext2D, x: number, y: number, width: number, height: number): void {
    context.beginPath()
    context.roundRect(x, y, Math.max(0, width), height, Math.min(cornerRadius, height / 2, Math.max(0, width) / 2))
  }

  function fillRoundedRect(
    context: CanvasRenderingContext2D,
    x: number,
    y: number,
    width: number,
    height: number,
    fill: string
  ): void {
    if (width <= 0) return
    roundedRect(context, x, y, width, height)
    context.fillStyle = fill
    context.fill()
  }

  function drawHatch(
    context: CanvasRenderingContext2D,
    x: number,
    y: number,
    width: number,
    height: number,
    color: string
  ): void {
    if (width <= 0) return
    context.save()
    roundedRect(context, x, y, width, height)
    context.clip()
    context.beginPath()
    for (let start = x - height; start < x + width + height; start += 6) {
      context.moveTo(start, y + height)
      context.lineTo(start + height, y)
    }
    context.globalAlpha = 0.42
    context.lineWidth = 1
    context.strokeStyle = color
    context.stroke()
    context.restore()
  }

  function paintTake(
    context: CanvasRenderingContext2D,
    take: Take,
    startX: number,
    pixelsPerMs: number,
    troughWidth: number,
    colors: Record<string, string>,
    ghost: boolean
  ): void {
    const measuredWidth = take.fit.measured_ms * pixelsPerMs
    const insideWidth = Math.min(measuredWidth, troughWidth)
    const overrunWidth = Math.max(0, measuredWidth - troughWidth)

    context.save()
    context.globalAlpha = ghost ? 0.28 : 1
    fillRoundedRect(context, startX, troughY, insideWidth, troughHeight, colors.fit)
    if (overrunWidth > 0) {
      fillRoundedRect(context, startX + troughWidth, troughY, overrunWidth, troughHeight, colors.over)
    }

    if (take.fit.state === "too_short") {
      fillRoundedRect(context, startX + insideWidth, troughY, troughWidth - insideWidth, troughHeight, colors.short)
    }

    if (take.repair === "atempo") {
      drawHatch(context, startX, troughY, insideWidth, troughHeight, colors.text)
      if (overrunWidth > 0) {
        drawHatch(context, startX + troughWidth, troughY, overrunWidth, troughHeight, colors.text)
      }
    }
    context.restore()
  }

  function repairLabel(take: Take | undefined): string {
    if (!take) return ""
    if (take.repair === "atempo" && take.stretch_factor_milli !== undefined) {
      const speed = take.stretch_factor_milli / 10
      return `${Number.isInteger(speed) ? speed.toFixed(0) : speed.toFixed(1)}% speed`
    }
    if (take.repair === "rewrite") return "rewritten"
    if (take.fit.state === "too_short") return `${durationText(take.fit.delta_ms)} short`
    if (take.fit.state === "too_long") return `${durationText(take.fit.delta_ms)} long`
    return "fits"
  }

  function repaint(): void {
    if (!canvas) return

    const bounds = canvas.getBoundingClientRect()
    const parentWidth = canvas.parentElement?.getBoundingClientRect().width
    const scaleWidth = Math.max(1, Math.floor(parentWidth ?? bounds.width))
    const ratio = Math.min(window.devicePixelRatio || 1, 3)
    const usableWidth = Math.max(1, scaleWidth - horizontalInset * 2)
    const pixelsPerMs = usableWidth / Math.max(1, scaleCeilingMs)
    const troughWidth = Math.min(slotMs * pixelsPerMs, usableWidth)
    const longestTakeMs = Math.max(slotMs, ...takes.map((take) => take.fit.measured_ms))
    const requiredPaintWidth = Math.ceil(horizontalInset * 2 + longestTakeMs * pixelsPerMs)

    const nextPaintWidth = Math.max(scaleWidth, requiredPaintWidth)
    if (paintWidth !== nextPaintWidth) {
      paintWidth = nextPaintWidth
      scheduleRepaint()
      return
    }

    const cssWidth = Math.max(1, Math.floor(bounds.width))
    canvas.width = Math.round(cssWidth * ratio)
    canvas.height = Math.round(canvasHeight * ratio)

    const context = canvas.getContext("2d")
    if (!context) return
    context.setTransform(ratio, 0, 0, ratio, 0, 0)
    context.clearRect(0, 0, cssWidth, canvasHeight)

    const colors = {
      sunken: token("--sunken"),
      line: token("--line"),
      fit: token("--fit"),
      over: token("--over"),
      short: token("--short"),
      text: token("--text"),
      dim: token("--dim")
    }
    const active = activeTake

    fillRoundedRect(context, horizontalInset, troughY, troughWidth, troughHeight, colors.sunken)
    roundedRect(context, horizontalInset, troughY, troughWidth, troughHeight)
    context.lineWidth = 1
    context.strokeStyle = colors.line
    context.stroke()

    for (const take of takes.slice(0, -1)) {
      paintTake(context, take, horizontalInset, pixelsPerMs, troughWidth, colors, true)
    }
    if (active) {
      paintTake(context, active, horizontalInset, pixelsPerMs, troughWidth, colors, false)
    }

    const label = repairLabel(active)
    if (label) {
      context.fillStyle = colors.dim
      context.font = "11px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
      context.textBaseline = "middle"
      context.fillText(label, horizontalInset, 39)
    }
  }

  function scheduleRepaint(): void {
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(repaint)
  }

  $effect(() => {
    slotMs
    takes
    referenceSlotMs
    theme
    scheduleRepaint()
  })

  onMount(() => {
    const resizeObserver = new ResizeObserver(scheduleRepaint)
    const themeObserver = new MutationObserver(scheduleRepaint)
    resizeObserver.observe(canvas.parentElement ?? canvas)
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"]
    })
    window.addEventListener("resize", scheduleRepaint)
    scheduleRepaint()

    return () => {
      cancelAnimationFrame(frame)
      resizeObserver.disconnect()
      themeObserver.disconnect()
      window.removeEventListener("resize", scheduleRepaint)
    }
  })
</script>

<canvas
  bind:this={canvas}
  aria-hidden="true"
  height={canvasHeight}
  style:width={paintWidth > 0 ? `${paintWidth}px` : "100%"}
></canvas>
<span class="screen-reader-text">{accessibleDescription}</span>

<style>
  canvas {
    display: block;
    height: 48px;
  }

  .screen-reader-text {
    clip: rect(0 0 0 0);
    clip-path: inset(50%);
    height: 1px;
    left: 0;
    overflow: hidden;
    position: fixed;
    top: 0;
    white-space: nowrap;
    width: 1px;
  }
</style>
