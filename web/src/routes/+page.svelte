<script lang="ts">
  import { onMount } from "svelte"
  import type { DubIndex, DubSummary, Readiness } from "$lib/types"

  let projects = $state<DubSummary[]>([])
  let loading = $state(true)
  let fetchError = $state("")

  function formatCost(nanodollars: number): string {
    return `$${(nanodollars / 1_000_000_000).toFixed(2)}`
  }

  function readinessLabel(readiness: Readiness): string {
    switch (readiness) {
      case "ready":
        return "Ready for review"
      case "review":
        return "In review"
      case "running":
        return "Dubbing in progress"
      case "pending":
      default:
        return "Queued to start"
    }
  }

  function readinessClass(readiness: Readiness): string {
    switch (readiness) {
      case "ready":
        return "status-ok"
      case "review":
      case "running":
        return "status-warn"
      case "pending":
      default:
        return "status-dim"
    }
  }

  onMount(async () => {
    try {
      const response = await fetch("/api/dubs")
      if (!response.ok) {
        fetchError = "Could not load project list from the server."
        return
      }

      const index = (await response.json()) as DubIndex
      projects = index.dubs
    } catch {
      fetchError = "Could not connect to the server."
    } finally {
      loading = false
    }
  })
</script>

<svelte:head>
  <title>Ajilamu · Projects</title>
</svelte:head>

<section class="page-shell">
  <div class="page-heading">
    <div>
      <p class="label">Projects</p>
      <h1>Your dubs</h1>
      <p>Open a project to check the picture, voice timing, and every cost.</p>
    </div>
    <a class="primary-action" href="/new">Create a dub</a>
  </div>

  <div class="project-list" aria-label="Dubs, newest first">
    {#if loading}
      <p class="state-message">Loading your dubs…</p>
    {:else if fetchError}
      <p class="state-message error">{fetchError}</p>
    {:else if projects.length === 0}
      <p class="state-message">You have no dubs yet. Create your first dub to get started.</p>
    {:else}
      {#each projects as project}
        <a class="project-row" href="/d/{encodeURIComponent(project.id)}">
          <div>
            <h2>{project.title}</h2>
            <p>{project.languages.join(", ")}</p>
          </div>
          <div class="project-meta">
            <span class={readinessClass(project.readiness)}>
              <span aria-hidden="true">●</span> {readinessLabel(project.readiness)}
            </span>
            <span class="numeric">{formatCost(project.total_nanodollars)}</span>
          </div>
        </a>
      {/each}
    {/if}
  </div>
</section>

<style>
  .page-shell {
    margin: 0 auto;
    max-width: 980px;
    padding: 48px 24px;
  }

  .page-heading {
    align-items: end;
    display: flex;
    justify-content: space-between;
    margin-bottom: 30px;
  }

  h1,
  h2,
  p {
    margin: 0;
  }

  h1 {
    font-size: 20px;
    letter-spacing: -0.02em;
    margin: 3px 0 6px;
  }

  .page-heading p:not(.label),
  .project-row p {
    color: var(--dim);
  }

  .primary-action {
    background: var(--accent);
    border-radius: var(--radius-control);
    color: var(--surface);
    padding: 7px 10px;
    text-decoration: none;
  }

  .project-list {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-container);
    overflow: hidden;
  }

  .state-message {
    color: var(--dim);
    font-size: 13px;
    padding: 24px 16px;
  }

  .state-message.error {
    color: var(--stop);
  }

  .project-row {
    align-items: center;
    display: flex;
    justify-content: space-between;
    padding: 16px;
    text-decoration: none;
  }

  .project-row:hover {
    background: var(--raised);
  }

  h2 {
    font-size: 14px;
    margin-bottom: 3px;
  }

  .project-row p,
  .project-meta {
    font-size: 11.5px;
  }

  .project-meta {
    align-items: end;
    color: var(--dim);
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .status-ok {
    color: var(--ok);
  }

  .status-warn {
    color: var(--warn);
  }

  .status-dim {
    color: var(--dim);
  }

  @media (max-width: 560px) {
    .page-shell {
      padding: 28px 16px;
    }

    .page-heading {
      align-items: start;
      flex-direction: column;
      gap: 16px;
    }
  }
</style>
