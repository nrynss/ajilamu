<script lang="ts">
  import { onMount } from "svelte"
  import "../lib/tokens.css"

  let { children } = $props()
  let theme = $state<"light" | "dark">("light")

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
  <div class="project-status" aria-label="Workspace readiness">
    <span class="status-dot" aria-hidden="true"></span>
    <span>Ready for review</span>
  </div>
  <div class="chrome-spacer"></div>
  <span class="budget numeric">$0.02 spent</span>
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
    background: var(--ok);
    border-radius: var(--radius-pill);
    display: inline-block;
    height: 7px;
    width: 7px;
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
