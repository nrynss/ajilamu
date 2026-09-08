<script lang="ts">
  import { onMount } from "svelte"
  import { page } from "$app/state"
  import { fixtureChrome, isFixtureID } from "$lib/fixture"
  import type { DubIndex, Readiness } from "$lib/types"
  import "../lib/tokens.css"

  type ChromeProject = { readiness: Readiness; total_nanodollars: number } | null | "not_found" | "lookup_failed"

  let { children } = $props()
  let theme = $state<"light" | "dark">("light")
  let sampleProject = $state<ChromeProject>(null)

  let projectID = $derived(page.params.id)
  let isFixture = $derived(isFixtureID(projectID))
  let fixtureFacts = $derived(isFixture && projectID ? fixtureChrome(projectID) : undefined)

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

  function formatCost(nanodollars: number): string {
    return `$${(nanodollars / 1_000_000_000).toFixed(2)}`
  }

  let chromeStatus = $derived.by(() => {
    if (!projectID) return ""
    if (fixtureFacts) return readinessLabel(fixtureFacts.readiness)
    if (sampleProject === "not_found") return "Unknown project"
    if (sampleProject === "lookup_failed") return "Could not load status"
    if (sampleProject) return readinessLabel(sampleProject.readiness)
    return "Queued to start"
  })

  let chromeBudget = $derived.by(() => {
    if (!projectID) return ""
    if (fixtureFacts) return `${formatCost(fixtureFacts.total_nanodollars)} spent`
    if (sampleProject === "not_found") return "$0.00 spent"
    if (sampleProject === "lookup_failed") return ""
    if (sampleProject) return `${formatCost(sampleProject.total_nanodollars)} spent`
    return "$0.00 spent"
  })

  let chromeStatusClass = $derived.by(() => {
    if (!projectID) return "status-dim"
    if (fixtureFacts) return readinessClass(fixtureFacts.readiness)
    if (sampleProject === "not_found") return "status-dim"
    if (sampleProject === "lookup_failed") return "status-dim"
    if (sampleProject) return readinessClass(sampleProject.readiness)
    return "status-dim"
  })

  function applyTheme(nextTheme: "light" | "dark") {
    theme = nextTheme
    document.documentElement.dataset.theme = nextTheme
    document.querySelector('meta[name="theme-color"]')?.setAttribute(
      "content",
      nextTheme === "dark" ? "#12110F" : "#F2F1EF"
    )
  }

  function toggleTheme() {
    applyTheme(theme === "light" ? "dark" : "light")
  }

  onMount(() => {
    const savedTheme = window.localStorage.getItem("ajilamu-theme")
    if (savedTheme === "light" || savedTheme === "dark") {
      applyTheme(savedTheme)
    }
  })

  $effect(() => {
    if (typeof localStorage !== "undefined") {
      localStorage.setItem("ajilamu-theme", theme)
    }
  })

  $effect(() => {
    const id = page.params.id
    if (!id || isFixtureID(id)) {
      sampleProject = null
      return
    }

    let active = true
    fetch("/api/dubs")
      .then((res) => {
        if (!res.ok) return Promise.resolve({ ok: false as const })
        return res.json().then((data: DubIndex) => ({ ok: true as const, data })).catch(() => ({ ok: false as const }))
      })
      .then((result) => {
        if (!active) return
        if (!result.ok || !Array.isArray(result.data.dubs)) {
          sampleProject = "lookup_failed"
          return
        }
        const match = result.data.dubs.find((project) => project.id === id)
        sampleProject = match
          ? { readiness: match.readiness, total_nanodollars: match.total_nanodollars }
          : "not_found"
      })
      .catch(() => {
        if (active) sampleProject = "lookup_failed"
      })

    return () => {
      active = false
    }
  })
</script>

<svelte:head>
  <title>Ajilamu</title>
  <meta
    name="description"
    content="A precise workspace for dubbing video in the languages your audience speaks."
  />
</svelte:head>

<header class="chrome">
  <a class="brand" href="/" aria-label="Ajilamu projects">Ajilamu</a>
  {#if chromeStatus}
    <div class="project-status {chromeStatusClass}" aria-label="Workspace readiness">
      <span class="status-dot" aria-hidden="true"></span>
      <span>{chromeStatus}</span>
    </div>
  {/if}
  <div class="chrome-spacer"></div>
  {#if chromeBudget}
    <span class="budget numeric">{chromeBudget}</span>
  {/if}
  <button class="theme-toggle" type="button" onclick={toggleTheme} aria-label="Switch to {theme === "light" ? "dark" : "light"} theme">
    {theme === "light" ? "Dark theme" : "Light theme"}
  </button>
  <a class="new-project" href="/new">New dub</a>
</header>

<main>
  {@render children()}
</main>

<style>
  .chrome {
    align-items: center;
    background: var(--surface);
    border-bottom: 1px solid var(--line);
    display: flex;
    gap: 14px;
    height: 46px;
    padding: 0 16px;
  }

  .brand {
    font-size: 14px;
    font-weight: 700;
    letter-spacing: -0.01em;
    text-decoration: none;
  }

  .project-status {
    align-items: center;
    color: var(--dim);
    display: flex;
    font-size: 11.5px;
    gap: 6px;
  }

  .status-dot {
    background: var(--dim);
    border-radius: var(--radius-pill);
    display: inline-block;
    height: 7px;
    width: 7px;
  }

  .project-status.status-ok .status-dot {
    background: var(--ok);
  }

  .project-status.status-warn .status-dot {
    background: var(--warn);
  }

  .project-status.status-dim .status-dot {
    background: var(--dim);
  }

  .chrome-spacer {
    flex: 1;
  }

  .budget {
    color: var(--dim);
    font-size: 11.5px;
  }

  .theme-toggle,
  .new-project {
    min-height: 28px;
    padding: 4px 9px;
    text-decoration: none;
  }

  .new-project {
    background: var(--accent);
    border: 1px solid var(--accent);
    border-radius: var(--radius-control);
    color: var(--surface);
  }

  @media (max-width: 560px) {
    .project-status,
    .budget {
      display: none;
    }
  }
</style>
