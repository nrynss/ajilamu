<script module lang="ts">
  import type { EventStage } from "../types"

  export interface ProcessingStep {
    stage: EventStage
    sentence: string
    lineNumber?: number
    language?: string
  }

  const stageNames: Record<EventStage, string> = {
    segmenting: "Finding the spoken lines",
    translating: "Preparing the translation",
    synthesizing: "Making the voice take",
    measuring: "Checking the take length",
    repairing: "Adjusting the take",
    assembling: "Placing the takes in the picture",
    exporting: "Saving the finished video"
  }

  export function processingStepName(stage: EventStage): string {
    return stageNames[stage]
  }
</script>

<script lang="ts">
  interface Props {
    step: ProcessingStep
  }

  let { step }: Props = $props()
  let detail = $derived([
    step.lineNumber ? `Line ${step.lineNumber}` : "",
    step.language ? step.language.toUpperCase() : ""
  ].filter(Boolean).join(" · "))
</script>

<aside class="processing-banner" aria-live="polite" aria-label="Processing is in progress">
  <div class="copy">
    <p class="eyebrow">Working now</p>
    <p class="step">{processingStepName(step.stage)}</p>
    <p class="sentence">{step.sentence}</p>
  </div>
  {#if detail}
    <span class="detail numeric">{detail}</span>
  {/if}
</aside>

<style>
  .processing-banner {
    align-items: flex-start;
    background: var(--accent-q);
    border: 1px solid var(--accent);
    border-radius: var(--radius-panel);
    color: var(--text);
    display: flex;
    gap: 12px;
    justify-content: space-between;
    padding: 10px 12px;
  }

  .copy,
  .copy p {
    margin: 0;
  }

  .eyebrow {
    color: var(--dim);
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .step {
    font-size: 13px;
    font-weight: 650;
    margin-top: 2px !important;
  }

  .sentence {
    color: var(--dim);
    font-size: 12.5px;
    margin-top: 2px !important;
  }

  .detail {
    color: var(--dim);
    flex: 0 0 auto;
    font-size: 11.5px;
    padding-top: 2px;
  }
</style>
