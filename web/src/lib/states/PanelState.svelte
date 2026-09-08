<script module lang="ts">
  import type { Snippet } from "svelte"

  export type PanelState =
    | { kind: "loading"; sentence?: string }
    | { kind: "empty"; sentence: string }
    | { kind: "error"; sentence: string }
    | { kind: "populated" }

  export const defaultLoadingSentence = "We are getting this panel ready."
</script>

<script lang="ts">
  interface Props {
    state: PanelState
    label: string
    children?: Snippet
  }

  let { state, label, children }: Props = $props()

  let message = $derived(
    state.kind === "loading" ? state.sentence ?? defaultLoadingSentence :
    state.kind === "populated" ? "" : state.sentence
  )

  let messageRole = $derived(state.kind === "error" ? "alert" : "status")
</script>

<section
  class="panel-state"
  aria-busy={state.kind === "loading"}
  aria-label={label}
  data-state={state.kind}
>
  {#if state.kind === "populated"}
    {@render children?.()}
  {:else}
    <p class:error={state.kind === "error"} class="message" role={messageRole}>
      {message}
    </p>
  {/if}
</section>

<style>
  .panel-state {
    min-height: 0;
  }

  .message {
    align-items: flex-start;
    background: var(--raised);
    border: 1px solid var(--line-soft);
    border-radius: var(--radius-panel);
    color: var(--dim);
    display: flex;
    font-size: 12.5px;
    line-height: 1.5;
    margin: 0;
    padding: 12px;
  }

  .message.error {
    border-color: var(--stop);
    color: var(--text);
  }
</style>
