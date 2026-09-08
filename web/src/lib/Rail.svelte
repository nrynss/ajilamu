<script lang="ts">
  import DetailsTab from "./tabs/DetailsTab.svelte"
  import HistoryTab from "./tabs/HistoryTab.svelte"
  import LinesTab from "./tabs/LinesTab.svelte"
  import type { Dub, Line, Take } from "./types"

  type RailTab = "lines" | "details" | "history"
  const tabs: ReadonlyArray<{ id: RailTab, label: string }> = [
    { id: "lines", label: "Lines" },
    { id: "details", label: "Details" },
    { id: "history", label: "History" }
  ]

  interface Props {
    dub: Dub
    selectedSegmentId?: number
    onselect?: (segmentID: number) => void
    onplaytake?: (take: Take) => void
  }

  let { dub, selectedSegmentId = 1, onselect, onplaytake }: Props = $props()
  let activeTab = $state<RailTab>("lines")
  let localSelectedSegmentId = $state(1)
  let activeTrack = $derived(dub.languages[0])
  let selectedSegment = $derived(dub.segments.find((segment) => segment.id === localSelectedSegmentId))
  let selectedLine = $derived(activeTrack?.lines.find((line) => line.segment_id === localSelectedSegmentId))
  let rows = $derived(dub.segments.map((segment) => {
    const line = activeTrack?.lines.find((candidate) => candidate.segment_id === segment.id)
    return { segment, line, take: line?.takes.at(-1) }
  }))

  function select(segmentID: number): void {
    localSelectedSegmentId = segmentID
    onselect?.(segmentID)
  }

  $effect(() => {
    localSelectedSegmentId = selectedSegmentId
  })

  function tabID(tab: RailTab): string {
    return `rail-${tab}`
  }

  function selectTab(tab: RailTab): void {
    activeTab = tab
  }

  function handleTabKeydown(event: KeyboardEvent, tab: RailTab): void {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return

    event.preventDefault()
    const index = tabs.findIndex((candidate) => candidate.id === tab)
    const nextIndex = event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? tabs.length - 1
        : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
    const next = tabs[nextIndex]
    selectTab(next.id)
    document.getElementById(`${tabID(next.id)}-tab`)?.focus()
  }
</script>

<aside class="rail" aria-label="Workspace details">
  <div class="tab-row" role="tablist" aria-label="Workspace information">
    {#each tabs as tab}
      {@const selected = activeTab === tab.id}
      <button
        type="button"
        role="tab"
        id={`${tabID(tab.id as RailTab)}-tab`}
        aria-controls={tabID(tab.id as RailTab)}
        aria-selected={selected}
        tabindex={selected ? 0 : -1}
        onclick={() => selectTab(tab.id)}
        onkeydown={(event) => handleTabKeydown(event, tab.id)}
      >{tab.label}</button>
    {/each}
  </div>

  <div class="panel" role="tabpanel" id={tabID(activeTab)} aria-labelledby={`${tabID(activeTab)}-tab`}>
    {#if activeTab === "lines"}
      <LinesTab {rows} selectedSegmentId={localSelectedSegmentId} onselect={select} {onplaytake} />
    {:else if activeTab === "details"}
      <DetailsTab segment={selectedSegment} line={selectedLine} total={dub.total} projectCharges={dub.charges} {onplaytake} />
    {:else}
      <HistoryTab projectId={dub.id} commits={dub.commits} lines={activeTrack?.lines ?? []} selectedSegmentId={localSelectedSegmentId} />
    {/if}
  </div>
</aside>

<style>
  .rail { background: var(--surface); border-left: 1px solid var(--line); display: flex; flex: 0 0 352px; flex-direction: column; min-width: 352px; width: 352px; }
  .tab-row { border-bottom: 1px solid var(--line-soft); display: flex; flex: 0 0 auto; }
  .tab-row button { background: transparent; border: 0; border-bottom: 2px solid transparent; border-radius: 0; color: var(--dim); flex: 1; font-size: 12.5px; padding: 12px 6px 10px; }
  .tab-row button[aria-selected="true"] { border-bottom-color: var(--accent); color: var(--text); }
  .tab-row button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
  .panel { flex: 1; min-height: 0; overflow: auto; }
  @media (max-width: 780px) { .rail { border-left: 0; border-top: 1px solid var(--line); flex: 0 0 auto; min-width: 0; width: 100%; } }
</style>
