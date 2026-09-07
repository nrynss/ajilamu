<svelte:head>
  <title>Ajilamu · Settings</title>
</svelte:head>

<section class="page-shell">
  <p class="label">Settings</p>
  <h1>Connect the services that make your dubs.</h1>
  <p class="intro">Credentials stay on this machine. We only use them when a step needs the service.</p>

  <form class="settings-form">
    <label for="voice-key">Voice service key</label>
    <input id="voice-key" type="password" autocomplete="off" placeholder="Add a key" />
    <label for="translation-key">Translation service key</label>
    <input id="translation-key" type="password" autocomplete="off" placeholder="Add a key" />
    <button type="submit">Save settings</button>
  </form>
</section>

<style>
  .page-shell {
    margin: 0 auto;
    max-width: 620px;
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
  }

  .settings-form {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-container);
    display: grid;
    gap: 8px;
    margin-top: 28px;
    padding: 16px;
  }

  label {
    color: var(--dim);
    font-size: 11.5px;
    margin-top: 8px;
  }

  input {
    background: var(--raised);
    border: 1px solid var(--line);
    color: var(--text);
    padding: 8px;
  }

  button {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--surface);
    justify-self: start;
    margin-top: 10px;
    padding: 7px 10px;
  }
</style>
