<svelte:head>
  <title>Ajilamu · Create a dub</title>
</svelte:head>

<section class="page-shell">
  <p class="label">New dub</p>
  <h1>Start with the picture.</h1>
  <p class="intro">Add your video, choose where it will play, then review the cost before any work begins.</p>

  <form class="create-form">
    <label class="drop-zone" for="video">
      <span class="drop-title">Drop a video here</span>
      <span class="drop-copy">or choose a video file from your computer</span>
      <input id="video" name="video" type="file" accept="video/*" />
    </label>

    <label class="music-option" for="music">
      <span>Add your music track</span>
      <span class="optional">Optional</span>
      <input id="music" name="music" type="file" accept="audio/*" />
    </label>

    <label class="field-label" for="languages">Languages</label>
    <select id="languages" name="languages">
      <option value="ml">Malayalam</option>
    </select>

    <div class="cost-note">
      <span class="label">Projected cost</span>
      <strong class="numeric">$0.02</strong>
      <p>This covers one segmenting pass, translations, and voice renders for this sample clip.</p>
    </div>

    <div class="actions">
      <a href="/d/fixture" class="sample-link">Try sample video, a NASA 75-second clip</a>
      <button class="start" type="submit">Start dubbing</button>
    </div>
  </form>
</section>

<style>
  .page-shell {
    margin: 0 auto;
    max-width: 660px;
    padding: 48px 24px;
  }

  h1,
  p {
    margin: 0;
  }

  h1 {
    font-size: 20px;
    letter-spacing: -0.02em;
    margin: 4px 0 6px;
  }

  .intro {
    color: var(--dim);
    max-width: 570px;
  }

  .create-form {
    display: grid;
    gap: 14px;
    margin-top: 28px;
  }

  .drop-zone {
    align-items: center;
    background: var(--surface);
    border: 1px dashed var(--line);
    border-radius: var(--radius-container);
    display: flex;
    flex-direction: column;
    gap: 4px;
    justify-content: center;
    min-height: 164px;
    padding: 24px;
    text-align: center;
  }

  .drop-title {
    font-size: 14px;
    font-weight: 650;
  }

  .drop-copy,
  .optional,
  .cost-note p {
    color: var(--dim);
    font-size: 11.5px;
  }

  input[type="file"] {
    color: var(--dim);
    margin-top: 8px;
    max-width: 100%;
  }

  .music-option {
    align-items: center;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 11px;
  }

  .music-option input {
    margin: 3px 0 0;
    width: 100%;
  }

  .field-label {
    color: var(--dim);
    font-size: 11.5px;
    margin-bottom: -8px;
  }

  select {
    background: var(--surface);
    border: 1px solid var(--line);
    color: var(--text);
    padding: 8px;
  }

  .cost-note {
    background: var(--accent-q);
    border-radius: var(--radius-panel);
    padding: 13px;
  }

  .cost-note strong {
    display: block;
    font-size: 16px;
    margin: 2px 0;
  }

  .actions {
    align-items: center;
    display: flex;
    gap: 14px;
    justify-content: space-between;
  }

  .sample-link {
    color: var(--accent);
    font-size: 11.5px;
  }

  .start {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--surface);
    padding: 7px 10px;
  }
</style>
