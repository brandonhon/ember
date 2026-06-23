<script lang="ts">
  import { onMount } from "svelte";
  import type { Filter, FilterMatch } from "../lib/types";
  import { api, ApiError } from "../lib/api";
  import { DEMO, notifyDemoBlocked } from "../demo/demo";
  import { boards, feeds } from "../lib/stores";

  let { onClose }: { onClose: () => void } = $props();

  let filters = $state<Filter[]>([]);
  let loading = $state(false);
  let error = $state("");
  let notice = $state("");
  let importInput: HTMLInputElement | undefined = $state();

  // Draft (used for both create and edit).
  type Draft = {
    id: number | null;
    name: string;
    field: FilterMatch["field"];
    op: FilterMatch["op"];
    value: string;
    case_sensitive: boolean;
    action: Filter["action"];
    enabled: boolean;
    priority: number;
    action_value: string;
  };
  const emptyDraft = (): Draft => ({
    id: null,
    name: "",
    field: "title",
    op: "contains",
    value: "",
    case_sensitive: false,
    action: "mark_read",
    enabled: true,
    priority: 100,
    action_value: "",
  });
  let draft = $state<Draft>(emptyDraft());

  // Preview state: how many of the last 7 days' articles would have hit
  // this rule. Cached for the current draft until the user changes any
  // match field; recomputes on a manual button press.
  let previewCount = $state<number | null>(null);
  let previewBusy = $state(false);
  let previewErr = $state("");

  // Reset preview whenever the match changes.
  $effect(() => {
    void draft.field;
    void draft.op;
    void draft.value;
    void draft.case_sensitive;
    previewCount = null;
    previewErr = "";
  });

  async function runPreview() {
    if (!draft.value.trim()) {
      previewErr = "value required to preview";
      return;
    }
    previewBusy = true;
    previewErr = "";
    try {
      const match: FilterMatch = {
        field: draft.field,
        op: draft.op,
        value: draft.value,
        case_sensitive: draft.case_sensitive,
      };
      const res = await api.previewFilter(JSON.stringify(match), 7);
      previewCount = res.data?.count ?? 0;
    } catch (e) {
      previewErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      previewBusy = false;
    }
  }

  async function doExport() {
    if (DEMO) {
      notifyDemoBlocked();
      return;
    }
    error = "";
    try {
      const res = await api.exportFilters();
      if (!res.ok) throw new Error("export failed");
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "ember-filters.json";
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function onImportPick(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    error = "";
    notice = "";
    try {
      const bundle = JSON.parse(await file.text());
      const res = await api.importFilters(bundle);
      await refresh();
      const { imported, skipped } = res.data;
      notice = `Imported ${imported} filter${imported === 1 ? "" : "s"}${skipped ? `, skipped ${skipped}` : ""}.`;
      setTimeout(() => (notice = ""), 4000);
    } catch (err) {
      error = err instanceof ApiError ? err.message : "Couldn't read that file — is it a valid filters backup?";
    } finally {
      input.value = "";
    }
  }

  async function refresh() {
    loading = true;
    error = "";
    try {
      const res = await api.listFilters();
      filters = res.data ?? [];
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  function startEdit(f: Filter) {
    const m = JSON.parse(f.match_json) as FilterMatch;
    draft = {
      id: f.id,
      name: f.name,
      field: m.field,
      op: m.op,
      value: m.value,
      case_sensitive: m.case_sensitive ?? false,
      action: f.action,
      enabled: f.enabled,
      priority: f.priority ?? 100,
      action_value: f.action_value ?? "",
    };
  }

  async function save() {
    // Demo: surface the "this is a demo" notice on the Add filter / Save click
    // before any validation, so it fires even with an empty draft.
    if (DEMO) { notifyDemoBlocked(); return; }
    if (!draft.name.trim() || !draft.value.trim()) {
      error = "name and value required";
      return;
    }
    const match: FilterMatch = {
      field: draft.field,
      op: draft.op,
      value: draft.value,
      case_sensitive: draft.case_sensitive,
    };
    const payload = {
      name: draft.name.trim(),
      match_json: JSON.stringify(match),
      action: draft.action,
      enabled: draft.enabled,
      priority: draft.priority,
      action_value: draft.action_value.trim(),
    };
    error = "";
    try {
      if (draft.id === null) {
        await api.createFilter(payload);
      } else {
        await api.updateFilter(draft.id, payload);
      }
      draft = emptyDraft();
      await refresh();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function remove(id: number) {
    try {
      await api.deleteFilter(id);
      await refresh();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function toggleEnabled(f: Filter) {
    try {
      await api.updateFilter(f.id, { enabled: !f.enabled });
      await refresh();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function describe(f: Filter): string {
    try {
      const m = JSON.parse(f.match_json) as FilterMatch;
      return `${m.field} ${m.op} "${m.value}"`;
    } catch {
      return f.match_json;
    }
  }
</script>

<div
  class="backdrop"
  role="dialog"
  aria-modal="true"
  aria-labelledby="filter-manager-title"
  on:click={onClose}
  data-testid="filter-manager"
>
  <div class="modal" on:click|stopPropagation>
    <header>
      <h2 id="filter-manager-title">Filters</h2>
      <div class="head-tools">
        <button type="button" on:click={doExport} data-testid="filters-export">Export</button>
        <button type="button" on:click={() => { if (DEMO) { notifyDemoBlocked(); return; } importInput?.click(); }} data-testid="filters-import">Import</button>
        <button class="close" on:click={onClose} aria-label="Close">×</button>
      </div>
      <input type="file" accept=".json,application/json" bind:this={importInput} on:change={onImportPick} style="display:none" data-testid="filters-import-input" />
    </header>

    {#if error}
      <p class="error" data-testid="filter-error">{error}</p>
    {/if}
    {#if notice}
      <p class="ok" data-testid="filters-notice">{notice}</p>
    {/if}

    <section class="form" aria-label="New or edit filter">
      <label>
        <span>Name</span>
        <input bind:value={draft.name} placeholder="Hide crypto news" data-testid="filter-name" />
      </label>
      <div class="row">
        <label>
          <span>Field</span>
          <select bind:value={draft.field} data-testid="filter-field">
            <option value="title">Title</option>
            <option value="content">Content</option>
            <option value="author">Author</option>
            <option value="url">URL</option>
            <option value="tags">Tags</option>
            <option value="feed_id">Feed</option>
            <option value="published_at">Published</option>
            <option value="has_image">Has image</option>
          </select>
        </label>
        <label>
          <span>Op</span>
          <select bind:value={draft.op} data-testid="filter-op">
            {#if draft.field === "feed_id" || draft.field === "has_image"}
              <option value="equals">equals</option>
            {:else if draft.field === "published_at"}
              <option value="newer_than">newer than</option>
            {:else}
              <option value="contains">contains</option>
              <option value="equals">equals</option>
              <option value="starts_with">starts with</option>
              <option value="matches">matches (regex)</option>
            {/if}
          </select>
        </label>
      </div>
      <label>
        <span>
          {#if draft.field === "feed_id"}Feed
          {:else if draft.field === "has_image"}Image present
          {:else if draft.field === "published_at"}Within (e.g. 24h, 7d)
          {:else}Value{/if}
        </span>
        {#if draft.field === "feed_id"}
          <select bind:value={draft.value} data-testid="filter-value">
            <option value="">— pick a feed —</option>
            {#each $feeds as feed (feed.id)}
              <option value={String(feed.id)}>{feed.title || feed.url}</option>
            {/each}
          </select>
        {:else if draft.field === "has_image"}
          <select bind:value={draft.value} data-testid="filter-value">
            <option value="true">true</option>
            <option value="false">false</option>
          </select>
        {:else}
          <input bind:value={draft.value} placeholder={draft.field === "published_at" ? "24h" : "crypto"} data-testid="filter-value" />
        {/if}
      </label>
      {#if draft.field !== "feed_id" && draft.field !== "has_image" && draft.field !== "published_at"}
        <label class="checkbox">
          <input type="checkbox" bind:checked={draft.case_sensitive} />
          <span>Case sensitive</span>
        </label>
      {/if}
      <div class="row" style="align-items: flex-end;">
        <button type="button" on:click={runPreview} disabled={previewBusy} data-testid="filter-preview">
          {previewBusy ? "Counting…" : "Preview matches (last 7 days)"}
        </button>
        {#if previewCount !== null}
          <span class="preview-result" data-testid="filter-preview-result">
            Would match {previewCount} article{previewCount === 1 ? "" : "s"}
          </span>
        {/if}
        {#if previewErr}
          <span class="preview-result" style="color: var(--ember);">{previewErr}</span>
        {/if}
      </div>
      <div class="row">
        <label>
          <span>Action</span>
          <select bind:value={draft.action} data-testid="filter-action">
            <option value="mark_read">Mark read</option>
            <option value="star">Star</option>
            <option value="hide">Hide (mark read)</option>
            <option value="tag">Tag</option>
            <option value="add_to_board">Add to board</option>
          </select>
        </label>
        {#if draft.action === "tag"}
          <label>
            <span>Tag name</span>
            <input bind:value={draft.action_value} placeholder="newsletters" data-testid="filter-action-value" />
          </label>
        {:else if draft.action === "add_to_board"}
          <label>
            <span>Board</span>
            <select bind:value={draft.action_value} data-testid="filter-action-value">
              <option value="">— pick a board —</option>
              {#each $boards as b (b.id)}
                <option value={String(b.id)}>{b.name}</option>
              {/each}
            </select>
          </label>
        {/if}
      </div>
      <div class="row">
        <label>
          <span>Priority (lower = earlier)</span>
          <input type="number" min="0" max="999" bind:value={draft.priority} data-testid="filter-priority" />
        </label>
        <label class="checkbox">
          <input type="checkbox" bind:checked={draft.enabled} />
          <span>Enabled</span>
        </label>
      </div>
      <div class="actions">
        {#if draft.id !== null}
          <button type="button" class="ghost" on:click={() => (draft = emptyDraft())}>
            Cancel
          </button>
        {/if}
        <button type="button" on:click={save} data-testid="filter-save">
          {draft.id === null ? "Add filter" : "Save changes"}
        </button>
      </div>
    </section>

    <hr />

    <section class="list">
      {#if loading}
        <p class="muted">Loading…</p>
      {:else if filters.length === 0}
        <p class="muted">No filters yet.</p>
      {:else}
        <ul>
          {#each filters as f (f.id)}
            <li data-testid="filter-row-{f.id}">
              <div class="info">
                <strong>{f.name}</strong>
                <span class="rule">{describe(f)}</span>
                <span class="action">→ {f.action}</span>
              </div>
              <div class="row-actions">
                <button type="button" class="ghost" on:click={() => toggleEnabled(f)}>
                  {f.enabled ? "Disable" : "Enable"}
                </button>
                <button type="button" class="ghost" on:click={() => startEdit(f)}>
                  Edit
                </button>
                <button type="button" class="ghost danger" on:click={() => remove(f.id)}>
                  Delete
                </button>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 5rem;
    z-index: 100;
  }
  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    width: min(640px, 95vw);
    max-height: 80vh;
    overflow-y: auto;
    padding: 1.25rem 1.5rem;
    color: var(--text);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.75rem;
  }
  h2 { margin: 0; font-family: "Fraunces", serif; }
  .close {
    background: transparent;
    border: 0;
    font-size: 1.5rem;
    cursor: pointer;
    color: var(--muted);
  }
  .close:hover { color: var(--ink); }
  .error {
    background: #fef2f2;
    color: #991b1b;
    border-radius: 4px;
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
  }
  .ok {
    background: color-mix(in srgb, var(--ember) 10%, transparent);
    color: var(--ember);
    border-radius: 4px;
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
  }
  .head-tools { display: flex; align-items: center; gap: 8px; }
  .form { display: flex; flex-direction: column; gap: 0.5rem; }
  label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85rem; }
  label.checkbox {
    flex-direction: row;
    align-items: center;
    gap: 0.4rem;
    color: var(--muted);
  }
  input[type="text"], input:not([type]), input[type="url"], select {
    padding: 0.4rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    font: inherit;
  }
  .row { display: flex; gap: 0.75rem; }
  .row label { flex: 1; }
  .preview-result {
    font-size: 0.85rem;
    color: var(--ink-faint);
    margin-left: 8px;
  }
  .actions {
    margin-top: 0.5rem;
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  /* Primary actions (Export / Import / Add filter / Save / Preview) —
     matches Settings `.actions button` / `.pack-btn`. */
  button {
    background: var(--ember);
    color: #fff;
    border: none;
    padding: 7px 14px;
    border-radius: 8px;
    cursor: pointer;
    font-family: var(--font-ui);
    font-size: 12.5px;
    font-weight: 600;
  }
  button:hover:not(:disabled) { background: var(--ember-soft); }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  /* Secondary actions (Disable / Edit / Cancel) — matches Settings `.ghost`. */
  button.ghost {
    background: transparent;
    color: var(--ink);
    border: 1px solid var(--line);
  }
  button.ghost:hover:not(:disabled) { background: var(--line-soft); }
  /* Destructive (Delete) — matches Settings `.btn-danger`. */
  button.ghost.danger {
    color: #b91c1c;
    border-color: #b91c1c;
  }
  button.ghost.danger:hover:not(:disabled) {
    background: #b91c1c;
    color: #fff;
  }
  hr { margin: 1rem 0; border: 0; border-top: 1px solid var(--border); }
  .list ul { list-style: none; padding: 0; margin: 0; }
  .list li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.5rem 0;
    border-bottom: 1px solid var(--border);
    gap: 1rem;
  }
  .info { display: flex; flex-direction: column; gap: 0.15rem; }
  .info .rule { color: var(--muted); font-size: 0.8rem; }
  .info .action { color: var(--accent); font-size: 0.75rem; text-transform: uppercase; }
  .row-actions { display: flex; gap: 0.25rem; }
  .muted { color: var(--muted); }
</style>
