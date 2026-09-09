<script lang="ts">
  import LanguagePicker from "$lib/LanguagePicker.svelte";

  const nanodollarsPerDollar = 1_000_000_000;

  // This reference is the completed P2 fixture ledger, not a made-up pipeline model.
  // testdata/wire/total.json records $0.023414 for its 75.008267-second NASA clip.
  // P2 records every segmentation, translation, and voice-rendering charge once.
  // The completed ledger remains the final cost after the job runs.
  const p2ReferenceDurationSeconds = 75.008267;
  const p2ReferenceNanodollars = 23_414_000;
  const projectedNanodollarsPerSecond = p2ReferenceNanodollars / p2ReferenceDurationSeconds;
  const formattedSampleFee = formatSampleCurrency(p2ReferenceNanodollars);

  let videoFile = $state<File | null>(null);
  let projectName = $state("");
  let musicFile = $state<File | null>(null);
  let durationSeconds = $state<number | null>(null);
  let estimateError = $state("");
  let submitError = $state("");
  let uploading = $state(false);
  let creatingSample = $state(false);
  let videoDragging = $state(false);
  let musicDragging = $state(false);
  let videoSelection = $state(0);

  // The source picker starts empty, because the film language may be unknown.
  // The target picker starts on Malayalam, the only language the sample uses.
  let sourceLanguage = $state("");
  let targetLanguage = $state("ml-IN");

  let projectedFee = $derived(durationSeconds === null ? null : projectFee(durationSeconds));
  let formattedFee = $derived(projectedFee === null ? "" : formatCurrency(projectedFee));
  let formattedDuration = $derived(durationSeconds === null ? "" : formatDuration(durationSeconds));

  function projectFee(seconds: number): number {
    return Math.round(seconds * projectedNanodollarsPerSecond) / nanodollarsPerDollar;
  }

  function formatCurrency(value: number): string {
    return new Intl.NumberFormat("en-US", {
      style: "currency",
      currency: "USD",
      minimumFractionDigits: 2,
      maximumFractionDigits: 2
    }).format(value);
  }

  function formatSampleCurrency(nanodollars: number): string {
    return new Intl.NumberFormat("en-US", {
      style: "currency",
      currency: "USD",
      minimumFractionDigits: 6,
      maximumFractionDigits: 6
    }).format(nanodollars / nanodollarsPerDollar);
  }

  function formatDuration(seconds: number): string {
    const rounded = Math.round(seconds);
    return `${Math.floor(rounded / 60)}m ${rounded % 60}s`;
  }

  async function chooseVideo(file: File | undefined): Promise<void> {
    if (!file) return;
    const selection = ++videoSelection;
    videoFile = file;
    durationSeconds = null;
    estimateError = "";
    submitError = "";
    const url = URL.createObjectURL(file);
    const probe = document.createElement("video");
    probe.preload = "metadata";
    probe.src = url;
    try {
      await new Promise<void>((resolve, reject) => {
        probe.onloadedmetadata = () => resolve();
        probe.onerror = () => reject(new Error("metadata unavailable"));
      });
      if (!Number.isFinite(probe.duration) || probe.duration <= 0) {
        throw new Error("invalid duration");
      }
      if (selection === videoSelection) durationSeconds = probe.duration;
    } catch {
      if (selection === videoSelection) {
        estimateError = "We could not read this video’s duration. Choose a playable video to see its projected fee.";
      }
    } finally {
      URL.revokeObjectURL(url);
    }
  }

  function chooseMusic(file: File | undefined): void {
    if (!file) return;
    musicFile = file;
    submitError = "";
  }

  function videoInput(event: Event): void {
    chooseVideo((event.currentTarget as HTMLInputElement).files?.[0]);
  }

  function musicInput(event: Event): void {
    chooseMusic((event.currentTarget as HTMLInputElement).files?.[0]);
  }

  function videoDrop(event: DragEvent): void {
    event.preventDefault();
    videoDragging = false;
    chooseVideo(event.dataTransfer?.files[0]);
  }

  function musicDrop(event: DragEvent): void {
    event.preventDefault();
    musicDragging = false;
    chooseMusic(event.dataTransfer?.files[0]);
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!videoFile || durationSeconds === null || uploading || creatingSample || !targetLanguage) return;
    uploading = true;
    submitError = "";
    const body = new FormData();
    body.append("video", videoFile);
    body.append("title", projectName);
    if (musicFile) body.append("music", musicFile);
    body.append("source_language", sourceLanguage);
    body.append("language", targetLanguage);

    try {
      const response = await fetch("/api/dubs/new", { method: "POST", body });
      if (!response.ok) throw new Error(await response.text());
      const uploaded = (await response.json()) as { id?: string };
      if (!uploaded.id) throw new Error("The upload did not return a project id.");
      window.location.assign(`/d/${encodeURIComponent(uploaded.id)}`);
    } catch (error) {
      submitError = error instanceof Error && error.message ? error.message : "The upload did not finish. Try again.";
      uploading = false;
    }
  }

  async function createSample(): Promise<void> {
    if (creatingSample || uploading) return;
    creatingSample = true;
    submitError = "";
    try {
      const response = await fetch("/api/dubs/sample", { method: "POST" });
      if (!response.ok) throw new Error(await response.text());
      const sample = (await response.json()) as { id?: string };
      if (!sample.id) throw new Error("The sample did not return a project id.");
      window.location.assign(`/d/${encodeURIComponent(sample.id)}`);
    } catch (error) {
      submitError = error instanceof Error && error.message ? error.message : "The sample did not start. Try again.";
      creatingSample = false;
    }
  }
</script>

<svelte:head>
  <title>Ajilamu · Create a dub</title>
</svelte:head>

<section class="page-shell">
  <p class="label">New dub</p>
  <h1>Start with the picture.</h1>
  <p class="intro">Add your video, choose where it will play, then review its fee before any work begins.</p>

  <form class="create-form" onsubmit={submit}>
    <label
      class:dragging={videoDragging}
      class="drop-zone"
      for="video"
      ondragover={(event) => event.preventDefault()}
      ondragenter={() => (videoDragging = true)}
      ondragleave={() => (videoDragging = false)}
      ondrop={videoDrop}
    >
      <span class="drop-title">{videoFile ? videoFile.name : "Drop a video here"}</span>
      <span class="drop-copy">{videoFile ? "Choose another video" : "or choose a video file from your computer"}</span>
      <input id="video" class="file-picker" name="video" type="file" accept="video/*" onchange={videoInput} />
    </label>

    <label
      class:dragging={musicDragging}
      class="music-option"
      for="music"
      ondragover={(event) => event.preventDefault()}
      ondragenter={() => (musicDragging = true)}
      ondragleave={() => (musicDragging = false)}
      ondrop={musicDrop}
    >
      <span class="drop-title">{musicFile ? musicFile.name : "Add your music track"}</span>
      <span class="optional">{musicFile ? "It will play cleanly beneath the dub." : "Optional"}</span>
      <input id="music" class="file-picker" name="music" type="file" accept="audio/*" onchange={musicInput} />
    </label>
    <label class="name-field" for="project-title">
      <span class="field-label">Project name</span>
      <input
        id="project-title"
        type="text"
        autocomplete="off"
        placeholder="Optional. Blank uses the video file name."
        bind:value={projectName}
      />
    </label>

    <LanguagePicker
      id="source-language"
      name="source_language"
      label="Source language"
      value={sourceLanguage}
      onlanguagechange={(language) => (sourceLanguage = language)}
    />

    <LanguagePicker
      id="target-language"
      name="language"
      label="Target language"
      value={targetLanguage}
      onlanguagechange={(language) => (targetLanguage = language)}
    />

    <div class="cost-note" aria-live="polite">
      <span class="label">Projected fee</span>
      {#if formattedFee}
        <strong class="numeric">{formattedFee}</strong>
        <p>Estimated from this video’s {formattedDuration} runtime. It includes one analysis pass, translation, and voice rendering.</p>
      {:else}
        <strong>Choose a video to estimate its fee.</strong>
        <p>The NASA sample has a measured fee of <span class="numeric">{formattedSampleFee}</span>.</p>
      {/if}
      {#if estimateError}<p class="error">{estimateError}</p>{/if}
    </div>

    {#if submitError}<p class="error" role="alert">{submitError}</p>{/if}

    <div class="actions">
      <div class="sample-action">
        <button
          class="sample"
          type="button"
          disabled={creatingSample || uploading}
          aria-busy={creatingSample}
          onclick={createSample}
        >
          {creatingSample ? "We are copying the NASA sample into your workspace." : "Try sample video (NASA 75s clip)"}
        </button>
        <p class="sample-fee">Measured fee: <span class="numeric">{formattedSampleFee}</span></p>
      </div>
      <button class="start" type="submit" disabled={!videoFile || durationSeconds === null || uploading || creatingSample || !targetLanguage}>
        {uploading ? "Uploading…" : "Start dubbing"}
      </button>
    </div>
  </form>
</section>

<style>
  .page-shell { margin: 0 auto; max-width: 660px; padding: 48px 24px; }
  h1, p { margin: 0; }
  h1 { font-size: 20px; letter-spacing: -0.02em; margin: 4px 0 6px; }
  .intro { color: var(--dim); max-width: 570px; }
  .create-form { display: grid; gap: 14px; margin-top: 28px; }

  .drop-zone, .music-option {
    background: var(--surface);
    border: 1px dashed var(--line);
    border-radius: var(--radius-container);
    cursor: pointer;
  }

  .drop-zone {
    align-items: center;
    display: flex;
    flex-direction: column;
    gap: 4px;
    justify-content: center;
    min-height: 164px;
    padding: 24px;
    text-align: center;
  }

  .music-option {
    align-items: center;
    border-style: solid;
    display: grid;
    gap: 4px 12px;
    grid-template-columns: 1fr auto;
    padding: 13px;
  }
  .name-field {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-container);
    display: grid;
    gap: 6px;
    padding: 13px;
  }

  .field-label { font-size: 12px; font-weight: 650; }
  .name-field input { background: var(--raised); border: 1px solid var(--line); color: var(--text); padding: 7px 9px; }

  .dragging { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent-q); }
  .drop-title { font-size: 14px; font-weight: 650; overflow-wrap: anywhere; }
  .drop-copy, .optional, .cost-note p { color: var(--dim); font-size: 11.5px; }
  .file-picker { color: var(--dim); font-size: 11.5px; margin-top: 8px; max-width: 100%; }
  .music-option .file-picker { grid-column: 1 / -1; margin-top: 2px; }
  .cost-note { background: var(--accent-q); border-radius: var(--radius-panel); padding: 13px; }
  .cost-note strong { display: block; font-size: 16px; margin: 2px 0; }
  .error { color: #7a271a !important; margin-top: 8px !important; }
  :global(:root[data-theme="dark"]) .error { color: #ffd3ce !important; }
  .actions { align-items: center; display: flex; gap: 14px; justify-content: space-between; }
  .sample-action { align-items: flex-start; display: flex; flex-direction: column; gap: 4px; }
  .sample { background: var(--surface); border: 1px solid var(--accent); color: var(--accent); font-size: 11.5px; padding: 7px 10px; }
  .sample-fee { color: var(--dim); font-size: 10px; }
  .start { background: var(--accent); border-color: var(--accent); color: var(--surface); padding: 7px 10px; }
  .sample:disabled { color: var(--accent); cursor: not-allowed; opacity: 1; }
  .start:disabled { cursor: not-allowed; opacity: 0.6; }

  @media (max-width: 480px) {
    .page-shell { padding: 32px 16px; }
    .actions { align-items: stretch; flex-direction: column; }
    .sample-action { align-items: stretch; }
    .sample, .start { width: 100%; }
  }
</style>
