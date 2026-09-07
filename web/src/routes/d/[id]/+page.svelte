<script lang="ts">
  import { page } from "$app/state"

  let projectID = $derived(page.params.id)
</script>

<svelte:head>
  <title>Ajilamu · Workspace</title>
</svelte:head>

<section class="workspace" aria-label="Dubbing workspace">
  <section class="picture-area" aria-label="Picture and playback">
    <div class="preview" role="img" aria-label="Video preview will appear here">
      <span>Picture preview</span>
    </div>
    <div class="playback-row">
      <button type="button" aria-label="Play video">Play</button>
      <span class="numeric">0:00.0 / 1:15.0</span>
      <div class="languages" aria-label="Target language playback">
        <span class="label">Playback</span>
        <button type="button" aria-pressed="true">Malayalam</button>
      </div>
    </div>
  </section>

  <aside class="details" aria-label="Details">
    <div class="tab-row" role="tablist" aria-label="Workspace information">
      <button type="button" role="tab" aria-selected="true">Lines</button>
      <button type="button" role="tab" aria-selected="false">Details</button>
      <button type="button" role="tab" aria-selected="false">History</button>
    </div>
    <div class="details-body">
      <p class="label">Project</p>
      <h1>NASA 75-second clip</h1>
      <p>This workspace is ready for your timing review.</p>
      <dl>
        <div><dt>Project ID</dt><dd class="numeric">{projectID}</dd></div>
        <div><dt>Target language</dt><dd>Malayalam</dd></div>
        <div><dt>Running cost</dt><dd class="numeric">$0.02</dd></div>
      </dl>
    </div>
  </aside>

  <section class="timeline-placeholder" aria-label="Timeline">
    <div class="timeline-header">
      <span class="label">Timeline</span>
      <input aria-label="Editor command" placeholder="Type a command to edit this timeline" />
    </div>
    <div class="ruler numeric"><span>0:00</span><span>0:15</span><span>0:30</span><span>0:45</span><span>1:00</span></div>
    <div class="track"><span>Original</span><div class="track-fill"></div></div>
    <div class="track"><span>Malayalam</span><div class="track-fill dub"></div></div>
  </section>
</section>

<style>
  .workspace {
    display: grid;
    gap: 0;
    grid-template-columns: minmax(0, 1fr) 352px;
    grid-template-rows: minmax(260px, 1fr) minmax(156px, 32vh);
    height: calc(100vh - 46px);
    min-height: 520px;
  }

  .picture-area {
    min-width: 0;
    padding: 20px 20px 14px;
  }

  .preview {
    align-items: center;
    aspect-ratio: 16 / 9;
    background: var(--sunken);
    border-radius: var(--radius-container);
    box-shadow: 0 8px 22px rgb(0 0 0 / 12%);
    color: var(--faint);
    display: flex;
    justify-content: center;
    max-height: calc(100% - 42px);
  }

  .playback-row {
    align-items: center;
    display: flex;
    gap: 10px;
    padding-top: 10px;
  }

  .playback-row button {
    padding: 4px 8px;
  }

  .playback-row > span {
    color: var(--dim);
    font-size: 11.5px;
  }

  .languages {
    align-items: center;
    display: flex;
    gap: 6px;
    margin-left: auto;
  }

  .languages button {
    background: var(--accent-q);
    border-color: var(--accent);
  }

  .details {
    background: var(--surface);
    border-left: 1px solid var(--line);
  }

  .tab-row {
    border-bottom: 1px solid var(--line-soft);
    display: flex;
  }

  .tab-row button {
    background: transparent;
    border: 0;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    color: var(--dim);
    padding: 12px 10px 10px;
  }

  .tab-row button[aria-selected="true"] {
    border-bottom-color: var(--accent);
    color: var(--text);
  }

  .details-body {
    padding: 16px;
  }

  h1,
  p {
    margin: 0;
  }

  h1 {
    font-size: 14px;
    margin: 4px 0;
  }

  .details-body > p:not(.label) {
    color: var(--dim);
    font-size: 11.5px;
  }

  dl {
    margin: 20px 0 0;
  }

  dl div {
    border-top: 1px solid var(--line-soft);
    display: grid;
    gap: 8px;
    grid-template-columns: 1fr auto;
    padding: 9px 0;
  }

  dt {
    color: var(--dim);
  }

  dd {
    margin: 0;
    text-align: right;
  }

  .timeline-placeholder {
    background: var(--surface);
    border-top: 1px solid var(--line);
    grid-column: 1 / -1;
    min-height: 156px;
    overflow: auto;
    padding: 12px 16px;
  }

  .timeline-header,
  .ruler,
  .track {
    align-items: center;
    display: grid;
    gap: 12px;
    grid-template-columns: 84px 1fr;
  }

  .timeline-header input {
    background: var(--raised);
    border: 1px solid var(--line);
    color: var(--text);
    min-width: 0;
    padding: 6px 8px;
  }

  .ruler {
    color: var(--faint);
    font-size: 10px;
    grid-template-columns: 84px repeat(5, 1fr);
    margin: 12px 0 5px;
  }

  .track {
    color: var(--dim);
    font-size: 11.5px;
    margin-top: 4px;
  }

  .track-fill {
    background: var(--faint);
    border-radius: var(--radius-control);
    height: 22px;
  }

  .track-fill.dub {
    background: var(--fit);
    width: 82%;
  }

  @media (max-width: 780px) {
    .workspace {
      grid-template-columns: 1fr;
      grid-template-rows: auto auto minmax(156px, 32vh);
      height: auto;
    }

    .picture-area {
      min-height: 300px;
    }

    .details {
      border-left: 0;
      border-top: 1px solid var(--line);
    }
  }
</style>
