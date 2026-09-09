<script lang="ts">
  import type { Charge, Line, Segment, Take, Total } from "$lib/types"

  interface Props {
    segment?: Segment
    line?: Line
    total: Total
    projectCharges: readonly Charge[]
    onplaytake?: (take: Take) => void
  }

  let { segment, line, total, projectCharges, onplaytake }: Props = $props()

  let activeTake = $derived(line?.takes.at(-1))
  let lineCharges = $derived(line?.takes.flatMap((take) => take.charges) ?? [])
  let lineCost = $derived(lineCharges.reduce((sum, charge) => sum + charge.total_nanodollars, 0))

  function formatDuration(milliseconds: number): string {
    return `${(milliseconds / 1000).toFixed(2)} seconds`
  }

  function formatMoney(nanodollars: number): string {
    const dollars = nanodollars / 1_000_000_000
    return `$${dollars.toFixed(9).replace(/0+$/, "").replace(/\.$/, ".00")}`
  }

  function repairSentence(take: Take): string {
    if (take.repair === "atempo" && take.stretch_factor_milli) {
      return `This take was sped up to ${(take.stretch_factor_milli / 10).toFixed(1)} percent.`
    }

    if (take.repair === "rewrite") {
      return "This take was rewritten before it was recorded."
    }

    if (take.repair === "manual") {
      return "You adjusted this take by hand."
    }

    if (take.fit.state === "too_short") {
      return `This take is ${formatDuration(Math.abs(take.fit.delta_ms))} short of the slot. The picture keeps going after the voice stops.`
    }

    if (take.fit.state === "too_long") {
      return `This take overruns the slot by ${formatDuration(take.fit.delta_ms)}.`
    }

    return "This one fits."
  }

  function chargeName(charge: Charge): string {
    switch (charge.kind) {
      case "segment":
        return "Finding lines"
      case "translate":
        return "Translation"
      case "synthesize":
        return "Voice render"
      case "agent":
        return "Editor agent turn"
      default: {
        const unhandledKind: never = charge.kind
        throw new Error(`Missing charge label for ${unhandledKind}`)
      }
    }
  }
  function unitSuffix(charge: Charge): string {
    if (!charge.unit) return ""
    return ` (${charge.unit.replace(/_/g, " ")})`
  }
</script>

<section class="details" aria-label="Line details">
  {#if segment && activeTake}
    <p class="eyebrow">Line <span class="numeric">{segment.id}</span></p>
    <h2>{segment.speaker}</h2>
    <p class="source">{segment.text}</p>

    <dl>
      <div><dt>Voice</dt><dd>{activeTake.voice}</dd></div>
      <div><dt>Source slot</dt><dd class="numeric">{formatDuration(segment.duration_ms)}</dd></div>
      <div>
        <dt>Active take</dt>
        <dd><button type="button" class="take-link numeric" onclick={() => onplaytake?.(activeTake!)}>{formatDuration(activeTake.fit.measured_ms)}</button></dd>
      </div>
      <div><dt>Tries</dt><dd class="numeric">{line?.takes.length ?? 0}</dd></div>
    </dl>

    <p class="repair">{repairSentence(activeTake)}</p>

    <h3>What this line cost</h3>
    <ul class="charges">
      {#each line?.takes ?? [] as take, takeIndex (`${take.file}-${takeIndex}`)}
        {#each take.charges as charge, chargeIndex (`${take.file}-${charge.kind}-${charge.units}-${charge.unit_price_nanodollars}-${charge.total_nanodollars}-${chargeIndex}`)}
          <li>
            <span>{chargeName(charge)}{unitSuffix(charge)} for try <span class="numeric">{take.attempt}</span></span>
            <span class="numeric">{formatMoney(charge.total_nanodollars)}</span>
          </li>
        {/each}
      {/each}
    </ul>
    <p class="line-total">This line has cost <span class="numeric">{formatMoney(lineCost)}</span> across every try.</p>
  {:else}
    <p class="empty">Choose a line to see its voice, timing, repairs, and costs.</p>
  {/if}

  <section class="project-total" aria-label="Project cost">
    <p class="eyebrow">Project cost</p>
    <strong class="numeric">{formatMoney(total.total_nanodollars)}</strong>
    <p>{total.covers}</p>
    {#if projectCharges.length > 0}
      <ul class="charges project-charges">
        {#each projectCharges as charge, chargeIndex (`project-${charge.kind}-${charge.units}-${charge.unit_price_nanodollars}-${charge.total_nanodollars}-${chargeIndex}`)}
          <li><span>{chargeName(charge)}{unitSuffix(charge)}</span><span class="numeric">{formatMoney(charge.total_nanodollars)}</span></li>
        {/each}
      </ul>
    {/if}
  </section>
</section>

<style>
  .details { padding: 16px; }
  .eyebrow { color: var(--dim); font-size: 10px; font-weight: 650; letter-spacing: .08em; margin: 0; text-transform: uppercase; }
  h2 { font-size: 14px; margin: 4px 0 2px; }
  h3 { font-size: 12.5px; margin: 18px 0 7px; }
  .source, .repair, .line-total, .project-total p, .empty { color: var(--dim); font-size: 11.5px; margin: 0; }
  dl { margin: 15px 0 0; }
  dl div { border-top: 1px solid var(--line-soft); display: grid; gap: 10px; grid-template-columns: 1fr minmax(0, 1.5fr); padding: 7px 0; }
  dt { color: var(--dim); }
  dd { margin: 0; overflow-wrap: anywhere; text-align: right; }
  .take-link { background: transparent; border: 0; color: var(--accent); cursor: pointer; padding: 0; text-decoration: underline; text-underline-offset: 3px; }
  .repair { background: var(--raised); border-left: 2px solid var(--accent); margin-top: 12px; padding: 8px 9px; }
  .charges { list-style: none; margin: 0; padding: 0; }
  .charges li { border-top: 1px solid var(--line-soft); display: flex; font-size: 11.5px; gap: 10px; justify-content: space-between; padding: 7px 0; }
  .charges li span:first-child { color: var(--dim); }
  .line-total { margin-top: 7px; }
  .project-total { border-top: 1px solid var(--line); margin-top: 18px; padding-top: 12px; }
  .project-total strong { display: block; font-size: 16px; margin: 3px 0; }
  .project-charges { margin-top: 8px; }
</style>
