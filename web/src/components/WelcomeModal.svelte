<script lang="ts">
  import { onMount, onDestroy } from "svelte";

  let { onClose }: { onClose: () => void } = $props();

  function browseStarterPacks() {
    // The Settings dialog's hash router watches for #starter on open.
    location.hash = "#starter";
    window.dispatchEvent(new CustomEvent("ember:open-settings", { detail: "starter" }));
    onClose();
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }
  onMount(() => window.addEventListener("keydown", onKey));
  onDestroy(() => window.removeEventListener("keydown", onKey));
</script>

<div
  class="backdrop"
  role="dialog"
  aria-modal="true"
  aria-labelledby="welcome-title"
  on:click={onClose}
  data-testid="welcome-modal"
>
  <div class="card" on:click|stopPropagation>
    <header>
      <h2 id="welcome-title">Welcome to Ember</h2>
      <button class="close" on:click={onClose} aria-label="Close">×</button>
    </header>
    <p>
      Ember is your self-hosted reader. Start by adding feeds — pick a curated
      pack to subscribe in one click, paste a feed URL into the sidebar
      <strong>+ Add feed</strong> button, or import an OPML file from another reader.
    </p>
    <div class="actions">
      <button class="primary" on:click={browseStarterPacks} data-testid="welcome-browse">
        Browse starter packs
      </button>
      <a
        class="docs-link"
        href="https://brandonhon.github.io/ember/getting-started"
        target="_blank"
        rel="noopener"
        data-testid="welcome-docs"
      >
        Read the docs ↗
      </a>
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    /* Start below the topbar — never cover it. New users land on this modal
       on first login (zero feeds) and need the topbar (user chip, theme
       toggle, refresh, sidebar collapse) to stay interactive without first
       dismissing the welcome card. The backdrop still catches outside clicks
       in the article/sidebar area for dismissal, and Esc / × button close it. */
    top: var(--topbar-h);
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.45);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 1.5rem 1.75rem;
    max-width: 460px;
    color: var(--text);
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.75rem;
  }
  h2 {
    margin: 0;
    font-family: "Fraunces", serif;
    font-size: 1.4rem;
  }
  .close {
    background: transparent;
    border: 0;
    cursor: pointer;
    font-size: 1.5rem;
    color: var(--muted);
  }
  p {
    margin: 0 0 1.25rem;
    line-height: 1.5;
    font-size: 0.95rem;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 1rem;
  }
  .primary {
    background: var(--ember);
    color: #fff;
    border: 0;
    padding: 0.5rem 1rem;
    border-radius: 6px;
    font-weight: 600;
    cursor: pointer;
    font-size: 0.9rem;
  }
  .primary:hover { background: var(--ember-soft); }
  .docs-link {
    color: var(--link, var(--ember));
    text-decoration: none;
    font-size: 0.9rem;
  }
  .docs-link:hover { text-decoration: underline; }
</style>
