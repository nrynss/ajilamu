<script lang="ts">
  import type { Commit, DubHistory, Line } from "$lib/types"

  interface Props {
    projectId: string
    commits: readonly Commit[]
    lines: readonly Line[]
    selectedSegmentId: number
  }

  let { projectId, commits, lines, selectedSegmentId }: Props = $props()

  // The ledger is the source of truth when it answers with commits.
  // The fixture commits stay as the fallback for a clone with no credentials.
  let fetchedCommits = $state<readonly Commit[]>([])
  let visibleCommits = $derived(fetchedCommits.length > 0 ? fetchedCommits : commits)

  $effect(() => {
    const id = projectId
    let active = true
    fetchedCommits = []
    fetch(`/api/dubs/${encodeURIComponent(id)}/history`)
      .then((response) => {
        if (!response.ok) return Promise.resolve<Commit[]>([])
        return response.json()
          .then((body: DubHistory) => (Array.isArray(body.commits) ? body.commits : []))
          .catch(() => [])
      })
      .then((commits) => {
        if (active) fetchedCommits = commits
      })
      .catch(() => {
        // Keep the fallback commits when the ledger is unreachable.
      })
    return () => {
      active = false
    }
  })

  let graph = $derived.by(() => {
    const byID = new Map(visibleCommits.map((commit) => [commit.commit_id, commit]))
    const children = new Map<string, Commit[]>()
    const roots: Commit[] = []
    const compare = (left: Commit, right: Commit) => left.created_at.localeCompare(right.created_at) || left.version_number - right.version_number

    for (const commit of visibleCommits) {
      if (!commit.parent_commit_id || !byID.has(commit.parent_commit_id)) {
        roots.push(commit)
        continue
      }
      const siblings = children.get(commit.parent_commit_id) ?? []
      siblings.push(commit)
      children.set(commit.parent_commit_id, siblings)
    }

    type GraphNode = { commit: Commit, lane: number, row: number }
    const nodes: GraphNode[] = []
    let nextLane = 0
    const visit = (commit: Commit, lane: number) => {
      nodes.push({ commit, lane, row: nodes.length })
      const descendants = (children.get(commit.commit_id) ?? []).sort(compare)
      for (const [position, child] of descendants.entries()) {
        // The first descendant continues the current branch. Each sibling gets
        // its own column, so an edge never passes through a sibling as ancestry.
        visit(child, position === 0 ? lane : nextLane++)
      }
    }
    for (const root of roots.sort(compare)) visit(root, nextLane++)

    const byCommitID = new Map(nodes.map((node) => [node.commit.commit_id, node]))
    const edges = nodes.flatMap((node) => {
      const parent = byCommitID.get(node.commit.parent_commit_id)
      return parent === undefined ? [] : [{ parent, child: node }]
    })
    return { nodes, edges, width: Math.max(28, nextLane * 26 + 2) }
  })

  let learnedVoice = $derived.by(() => {
    const activeTakes = lines.flatMap((line) => {
      const take = line.takes.at(-1)
      return take ? [take] : []
    })
    if (activeTakes.length === 0) return undefined

    const voices = new Map<string, typeof activeTakes>()
    for (const take of activeTakes) voices.set(take.voice, [...(voices.get(take.voice) ?? []), take])
    const [voice, takes] = [...voices.entries()].sort((left, right) => right[1].length - left[1].length)[0]
    const measured = takes.reduce((sum, take) => sum + take.fit.measured_ms, 0)
    const slots = takes.reduce((sum, take) => sum + take.fit.slot_ms, 0)
    return { voice, count: takes.length, percent: ((measured / slots) - 1) * 100 }
  })

  function actionSentence(commit: Commit): string {
    if (commit.instruction) return commit.instruction
    const actions: Record<Commit["action"], string> = {
      segment_created: "The source lines were saved.",
      boundary_nudged: "A line boundary was adjusted.",
      speaker_reassigned: "Who is speaking was corrected.",
      text_corrected: "The line text was corrected.",
      take_rendered: "A voice take was recorded.",
      atempo_stretched: "A take was sped up to fit its slot.",
      line_rewritten: "A line was rewritten and recorded again.",
      user_command: "An editor command was saved."
    }
    return actions[commit.action]
  }

  function timeLabel(timestamp: string): string {
    return new Intl.DateTimeFormat("en", { hour: "2-digit", minute: "2-digit", timeZone: "UTC" }).format(new Date(timestamp)) + " UTC"
  }
</script>

<section class="history" aria-label="Saved history">
  <p class="intro">Line <span class="numeric">{selectedSegmentId}</span> is selected. This is the saved history for the whole project.</p>
  {#if learnedVoice}
    <section class="voice-evidence" aria-label="What we learned about this voice">
      <p class="eyebrow">What we learned about this voice</p>
      <p>Your usual <span class="numeric">{learnedVoice.voice}</span> take runs {Math.abs(learnedVoice.percent).toFixed(1)} percent {learnedVoice.percent < 0 ? "shorter" : "longer"} than its source slots. That is from your last <span class="numeric">{learnedVoice.count}</span> recorded lines.</p>
    </section>
  {/if}
  {#if graph.nodes.length > 0}
    <div class="graph" style={`--graph-height: ${graph.nodes.length * 68}px; --graph-width: ${graph.width}px`}>
      <svg aria-hidden="true" class="edges" viewBox={`0 0 ${graph.width} ${graph.nodes.length * 68}`}>
        {#each graph.edges as edge (`${edge.parent.commit.commit_id}-${edge.child.commit.commit_id}`)}
          <path d={`M ${edge.parent.lane * 26 + 7} ${edge.parent.row * 68 + 13} H ${edge.child.lane * 26 + 7} V ${edge.child.row * 68 + 13}`} />
        {/each}
      </svg>
      <ol>
      {#each graph.nodes as node (node.commit.commit_id)}
        {@const commit = node.commit}
        <li style={`--lane: ${node.lane}`}>
          <span class="node" aria-hidden="true"></span>
          <div>
            <p class="version">Saved <span class="numeric">{commit.version_number}</span> <span class="numeric">{commit.commit_id.slice(0, 8)}</span></p>
            <p>{actionSentence(commit)}</p>
            <p class="metadata">{timeLabel(commit.created_at)} · {commit.author === "manual_ui" ? "you" : commit.author === "command_bar" ? "editor command" : "Ajilamu"}</p>
          </div>
        </li>
      {/each}
      </ol>
    </div>
  {:else}
    <p class="empty">There are no saved changes yet.</p>
  {/if}
</section>

<style>
  .history { padding: 16px; }
  .intro, .empty { color: var(--dim); font-size: 11.5px; margin: 0 0 15px; }
  .voice-evidence { background: var(--raised); border-left: 2px solid var(--accent); margin: 0 0 15px; padding: 9px; }
  .voice-evidence p:last-child { color: var(--dim); line-height: 1.45; margin-top: 4px; }
  .eyebrow { color: var(--dim); font-size: 10px; font-weight: 650; letter-spacing: .08em; text-transform: uppercase; }
  .graph { min-height: var(--graph-height); position: relative; }
  .edges { height: var(--graph-height); left: 0; overflow: visible; position: absolute; top: 0; width: var(--graph-width); }
  .edges path { fill: none; stroke: var(--line); stroke-width: 1; vector-effect: non-scaling-stroke; }
  ol { list-style: none; margin: 0; padding: 0; position: relative; }
  li { display: grid; gap: 10px; grid-template-columns: 13px minmax(0, 1fr); margin-left: calc(var(--lane) * 26px); position: relative; }
  li { min-height: 68px; }
  .node { background: var(--surface); border: 2px solid var(--accent); border-radius: var(--radius-pill); height: 13px; margin-top: 3px; width: 13px; z-index: 1; }
  li:last-child .node { background: var(--accent); }
  li div { border-bottom: 1px solid var(--line-soft); padding-bottom: 11px; }
  p { font-size: 11.5px; margin: 0; }
  .version { color: var(--text); font-weight: 650; }
  .metadata { color: var(--faint); font-size: 10px; margin-top: 3px; }
</style>
