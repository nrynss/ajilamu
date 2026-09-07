<script lang="ts">
  const projects = [
    {
      id: "fixture",
      title: "NASA 75-second clip",
      languages: "Malayalam",
      updated: "Ready for review"
    }
  ]
</script>

<svelte:head>
  <title>Ajilamu · Projects</title>
</svelte:head>

<section class="page-shell">
  <div class="page-heading">
    <div>
      <p class="label">Projects</p>
      <h1>Your dubs</h1>
      <p>Open a project to check the picture, voice timing, and every cost.</p>
    </div>
    <a class="primary-action" href="/new">Create a dub</a>
  </div>

  <div class="project-list" aria-label="Dubs, newest first">
    {#each projects as project}
      <a class="project-row" href="/d/{project.id}">
        <div>
          <h2>{project.title}</h2>
          <p>{project.languages}</p>
        </div>
        <div class="project-meta">
          <span class="ready"><span aria-hidden="true">●</span> {project.updated}</span>
          <span class="numeric">$0.02</span>
        </div>
      </a>
    {/each}
  </div>
</section>

<style>
  .page-shell {
    margin: 0 auto;
    max-width: 980px;
    padding: 48px 24px;
  }

  .page-heading {
    align-items: end;
    display: flex;
    justify-content: space-between;
    margin-bottom: 30px;
  }

  h1,
  h2,
  p {
    margin: 0;
  }

  h1 {
    font-size: 20px;
    letter-spacing: -0.02em;
    margin: 3px 0 6px;
  }

  .page-heading p:not(.label),
  .project-row p {
    color: var(--dim);
  }

  .primary-action {
    background: var(--accent);
    border-radius: var(--radius-control);
    color: var(--surface);
    padding: 7px 10px;
    text-decoration: none;
  }

  .project-list {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-container);
    overflow: hidden;
  }

  .project-row {
    align-items: center;
    display: flex;
    justify-content: space-between;
    padding: 16px;
    text-decoration: none;
  }

  .project-row:hover {
    background: var(--raised);
  }

  h2 {
    font-size: 14px;
    margin-bottom: 3px;
  }

  .project-row p,
  .project-meta {
    font-size: 11.5px;
  }

  .project-meta {
    align-items: end;
    color: var(--dim);
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .ready {
    color: var(--ok);
  }

  @media (max-width: 560px) {
    .page-shell {
      padding: 28px 16px;
    }

    .page-heading {
      align-items: start;
      flex-direction: column;
      gap: 16px;
    }
  }
</style>
