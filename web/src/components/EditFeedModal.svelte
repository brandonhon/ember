<script lang="ts">
  import type { Category, FeedWithCounts } from "../lib/types";

  let { feed, categories, onSave, onClose }: {
    feed: FeedWithCounts;
    categories: Category[];
    onSave: (req: { title_override?: string; category_id?: number; clear_category?: boolean; url?: string }) => Promise<void>;
    onClose: () => void;
  } = $props();

  // Seed from the current subscription. title shows the override if set, else
  // the feed's own title (editing it sets an override). folder 0 = no folder.
  let title = $state(feed.title_override || feed.title || "");
  let folder = $state<number>(feed.category_id ?? 0);
  let url = $state(feed.url || "");
  let busy = $state(false);
  let error = $state("");

  async function save() {
    error = "";
    busy = true;
    try {
      const req: { title_override?: string; category_id?: number; clear_category?: boolean; url?: string } = {};
      const trimmedTitle = title.trim();
      if (trimmedTitle !== (feed.title_override || feed.title || "")) {
        req.title_override = trimmedTitle;
      }
      if (folder !== (feed.category_id ?? 0)) {
        if (folder === 0) req.clear_category = true;
        else req.category_id = folder;
      }
      const trimmedURL = url.trim();
      if (trimmedURL && trimmedURL !== feed.url) {
        req.url = trimmedURL;
      }
      await onSave(req);
      onClose();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="backdrop" role="dialog" aria-modal="true" aria-labelledby="edit-feed-title" on:click={onClose} data-testid="edit-feed">
  <div class="modal" on:click|stopPropagation>
    <header>
      <h2 id="edit-feed-title">Edit feed</h2>
      <button class="close" on:click={onClose} aria-label="Close">×</button>
    </header>

    {#if error}
      <p class="error" data-testid="edit-feed-error">{error}</p>
    {/if}

    <label class="field">
      <span>Title</span>
      <input type="text" bind:value={title} disabled={busy} data-testid="edit-feed-title-input" />
    </label>

    <label class="field">
      <span>Folder</span>
      <select bind:value={folder} disabled={busy} data-testid="edit-feed-folder">
        <option value={0}>No folder</option>
        {#each categories as c (c.id)}
          <option value={c.id}>{c.name}</option>
        {/each}
      </select>
    </label>

    <label class="field">
      <span>Feed URL</span>
      <input type="url" bind:value={url} disabled={busy} data-testid="edit-feed-url" />
      <small>Changing the URL re-points this subscription and re-fetches it.</small>
    </label>

    <div class="actions">
      <button class="ghost" on:click={onClose} disabled={busy}>Cancel</button>
      <button on:click={save} disabled={busy} data-testid="edit-feed-save">
        {busy ? "Saving…" : "Save"}
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
    width: min(440px, 95vw);
    padding: 1.25rem 1.5rem;
    color: var(--text);
  }
  header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; }
  h2 { margin: 0; font-family: "Fraunces", serif; }
  .close { background: transparent; border: 0; font-size: 1.5rem; cursor: pointer; color: var(--muted); }
  .error { background: #fef2f2; color: #991b1b; border-radius: 4px; padding: 0.5rem 0.75rem; font-size: 0.85rem; }
  .field { display: flex; flex-direction: column; gap: 0.25rem; margin-bottom: 0.85rem; }
  .field span { font-size: 0.8rem; color: var(--muted); }
  .field input, .field select {
    background: var(--bg); color: var(--text);
    border: 1px solid var(--border); border-radius: 4px;
    padding: 0.45rem 0.6rem; font: inherit;
  }
  .field small { color: var(--muted); font-size: 0.72rem; }
  .actions { display: flex; justify-content: flex-end; gap: 0.5rem; margin-top: 0.25rem; }
  .actions button {
    background: var(--accent); color: white; border: 0;
    padding: 0.4rem 0.85rem; border-radius: 4px; cursor: pointer; font: inherit;
  }
  .actions button.ghost { background: transparent; color: inherit; border: 1px solid var(--border); }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
