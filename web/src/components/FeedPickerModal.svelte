<script lang="ts">
  import type { DiscoveredFeed } from "../lib/types";

  let { feeds, onAdd, onClose }: {
    feeds: DiscoveredFeed[];
    onAdd: (urls: string[]) => Promise<void>;
    onClose: () => void;
  } = $props();

  // Default every discovered feed to selected.
  let selected = $state<Set<string>>(new Set(feeds.map((f) => f.url)));
  let busy = $state(false);
  let error = $state("");

  function toggle(url: string) {
    const next = new Set(selected);
    if (next.has(url)) next.delete(url);
    else next.add(url);
    selected = next;
  }

  async function add() {
    const urls = feeds.map((f) => f.url).filter((u) => selected.has(u));
    if (urls.length === 0) return;
    error = "";
    busy = true;
    try {
      await onAdd(urls);
      onClose();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  function label(f: DiscoveredFeed): string {
    return f.title?.trim() || f.url;
  }
</script>

<div
  class="backdrop"
  role="dialog"
  aria-modal="true"
  aria-labelledby="feed-picker-title"
  on:click={onClose}
  data-testid="feed-picker"
>
  <div class="modal" on:click|stopPropagation>
    <header>
      <h2 id="feed-picker-title">Multiple feeds found</h2>
      <button class="close" on:click={onClose} aria-label="Close">×</button>
    </header>
    <p class="subject">Pick the feed(s) you want to add.</p>

    {#if error}
      <p class="error" data-testid="feed-picker-error">{error}</p>
    {/if}

    <ul class="feeds">
      {#each feeds as f (f.url)}
        <li>
          <label data-testid="feed-picker-item">
            <input
              type="checkbox"
              checked={selected.has(f.url)}
              on:change={() => toggle(f.url)}
              disabled={busy}
            />
            <span class="feed-label">
              <span class="feed-title">{label(f)}</span>
              {#if f.type}<span class="feed-type">{f.type}</span>{/if}
              <span class="feed-url">{f.url}</span>
            </span>
          </label>
        </li>
      {/each}
    </ul>

    <div class="actions">
      <button class="ghost" on:click={onClose} disabled={busy}>Cancel</button>
      <button on:click={add} disabled={busy || selected.size === 0} data-testid="feed-picker-add">
        {busy ? "Adding…" : `Add ${selected.size} feed${selected.size === 1 ? "" : "s"}`}
      </button>
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed; inset: 0;
    background: rgba(0, 0, 0, 0.45);
    display: flex; align-items: flex-start; justify-content: center;
    padding-top: 5rem; z-index: 100;
  }
  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    width: min(480px, 95vw);
    padding: 1.25rem 1.5rem;
    color: var(--text);
  }
  header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; }
  h2 { margin: 0; font-family: "Fraunces", serif; }
  .close { background: transparent; border: 0; font-size: 1.5rem; cursor: pointer; color: var(--muted); }
  .subject { color: var(--muted); margin: 0 0 0.75rem; font-size: 0.85rem; }
  .error { background: #fef2f2; color: #991b1b; border-radius: 4px; padding: 0.5rem 0.75rem; font-size: 0.85rem; }
  .feeds { list-style: none; margin: 0 0 0.85rem; padding: 0; max-height: 50vh; overflow-y: auto; }
  .feeds li { border-bottom: 1px solid var(--border); }
  .feeds li:last-child { border-bottom: 0; }
  label { display: flex; align-items: flex-start; gap: 0.5rem; padding: 0.55rem 0.1rem; cursor: pointer; }
  input[type="checkbox"] { margin-top: 0.2rem; }
  .feed-label { display: flex; flex-direction: column; gap: 0.1rem; min-width: 0; }
  .feed-title { font-weight: 600; font-size: 0.9rem; word-break: break-word; }
  .feed-type {
    align-self: flex-start;
    font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.04em;
    color: var(--ember); border: 1px solid var(--border); border-radius: 3px;
    padding: 0 4px;
  }
  .feed-url { color: var(--muted); font-size: 0.75rem; word-break: break-all; }
  .actions { display: flex; justify-content: flex-end; gap: 0.5rem; }
  .actions button {
    background: var(--accent); color: white; border: 0;
    padding: 0.4rem 0.85rem; border-radius: 4px; cursor: pointer; font: inherit;
  }
  .actions button.ghost { background: transparent; color: inherit; border: 1px solid var(--border); }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
