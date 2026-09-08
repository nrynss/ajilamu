<script lang="ts">
  import { onMount } from "svelte"
  import type { LanguageCatalog } from "./types"

  interface Props {
    id: string
    name: string
    label: string
    value: string
    onlanguagechange?: (language: string) => void
  }

  let { id, name, label, value, onlanguagechange }: Props = $props()

  let languages = $state<string[]>([])
  let source = $state("")
  let loading = $state(true)
  let refreshing = $state(false)
  let error = $state("")

  // The catalog arrives from the server. The committed list needs no
  // credentials, so a clone with none still fills the picker.
  onMount(() => {
    let active = true
    fetch("/api/languages")
      .then((response) => {
        if (!response.ok) throw new Error(`The language list did not load (${response.status}).`)
        return response.json() as Promise<LanguageCatalog>
      })
      .then((catalog) => {
        if (!active) return
        languages = Array.isArray(catalog.languages) ? catalog.languages : []
        source = catalog.source
      })
      .catch((reason: unknown) => {
        if (!active) return
        error = reason instanceof Error && reason.message ? reason.message : "The language list did not load."
      })
      .finally(() => {
        if (active) loading = false
      })
    return () => {
      active = false
    }
  })

  // The creator chooses whether to pull the live list from the provider.
  async function refresh(): Promise<void> {
    if (refreshing) return
    refreshing = true
    error = ""
    try {
      const response = await fetch("/api/languages/refresh", { method: "POST" })
      if (!response.ok) {
        throw new Error((await response.text()).trim() || `The live language list did not load (${response.status}).`)
      }
      const catalog = (await response.json()) as LanguageCatalog
      if (Array.isArray(catalog.languages) && catalog.languages.length > 0) {
        languages = catalog.languages
      }
      source = catalog.source
    } catch (reason) {
      // The server keeps the cached list, so the picker keeps it too.
      error = reason instanceof Error && reason.message ? reason.message : "The live language list did not load."
    } finally {
      refreshing = false
    }
  }

  function labelFor(language: string): string {
    if (typeof Intl.DisplayNames === "function") {
      return new Intl.DisplayNames(["en"], { type: "language" }).of(language) ?? language
    }
    return language.toUpperCase()
  }

  let status = $derived(
    loading
      ? "Loading languages."
      : source === "provider"
        ? `${languages.length} live languages from Cloud Text-to-Speech.`
        : `${languages.length} bundled languages. Fetch the live list to update them.`
  )
</script>

<div class="language-picker">
  <label for={id}>{label}</label>
  <div class="controls">
    <select
      {id}
      {name}
      value={value}
      disabled={loading || languages.length === 0}
      onchange={(event) => onlanguagechange?.((event.currentTarget as HTMLSelectElement).value)}
    >
      {#if !value}
        <option value="">Choose a language</option>
      {:else if !languages.includes(value)}
        <option value={value}>{labelFor(value)}</option>
      {/if}
      {#each languages as language (language)}
        <option value={language}>{labelFor(language)}</option>
      {/each}
    </select>
    <button type="button" onclick={refresh} disabled={refreshing}>
      {refreshing ? "Fetching…" : "Fetch live languages"}
    </button>
  </div>
  <p class="status" aria-live="polite">{status}</p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</div>

<style>
  .language-picker {
    display: grid;
    gap: 5px;
  }

  label {
    color: var(--dim);
    font-size: 11.5px;
  }

  .controls {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  select {
    background: var(--raised);
    border: 1px solid var(--line);
    color: var(--text);
    min-width: 190px;
    padding: 7px 8px;
  }

  button {
    background: var(--raised);
    font-size: 12.5px;
    padding: 6px 9px;
  }

  .status {
    color: var(--dim);
    font-size: 11.5px;
    margin: 0;
  }

  .error {
    color: var(--stop);
    font-size: 11.5px;
    margin: 0;
  }
</style>
