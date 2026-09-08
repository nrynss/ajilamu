<script lang="ts">
  import type { LanguageTrack } from "./types"

  interface Props {
    languages: readonly LanguageTrack[]
    activeLanguage: string
    onlanguagechange?: (language: string) => void
  }

  let { languages, activeLanguage, onlanguagechange }: Props = $props()

  function labelFor(language: string): string {
    if (typeof Intl.DisplayNames === "function") {
      return new Intl.DisplayNames(["en"], { type: "language" }).of(language) ?? language
    }

    return language.toUpperCase()
  }

  function selectLanguage(language: string): void {
    if (language !== activeLanguage) {
      onlanguagechange?.(language)
    }
  }
</script>

<section class="language-strip" aria-label="Dubbed audio playback">
  <span class="label">Playback audio</span>

  {#if languages.length > 0}
    <div class="choices" role="group" aria-label="Choose dubbed audio">
      {#each languages as track (track.language)}
        {@const selected = track.language === activeLanguage}
        <button
          type="button"
          aria-pressed={selected}
          aria-label={`Hear ${labelFor(track.language)}`}
          onclick={() => selectLanguage(track.language)}
        >
          {labelFor(track.language)}
        </button>
      {/each}
    </div>
    <p class="active" aria-live="polite">Now hearing {labelFor(activeLanguage)}.</p>
  {:else}
    <p class="empty">No dubbed tracks are ready to play yet.</p>
  {/if}
</section>

<style>
  .language-strip {
    align-items: center;
    border-top: 1px solid var(--line-soft);
    display: flex;
    flex-wrap: wrap;
    gap: 7px 10px;
    min-height: 42px;
    padding-top: 9px;
  }

  .choices {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }

  button {
    background: var(--raised);
    font-size: 12.5px;
    line-height: 1.2;
    padding: 5px 8px;
  }

  button[aria-pressed="true"] {
    background: var(--accent-q);
    border-color: var(--accent);
    color: var(--text);
  }

  .active,
  .empty {
    color: var(--dim);
    font-size: 11.5px;
    margin: 0 0 0 auto;
  }

  @media (max-width: 560px) {
    .active,
    .empty {
      margin-left: 0;
      width: 100%;
    }
  }
</style>
