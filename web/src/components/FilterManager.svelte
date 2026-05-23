<script lang="ts">
  import { onMount } from "svelte";
  import type { Filter, FilterMatch } from "../lib/types";
  import { api, ApiError } from "../lib/api";

  let { onClose }: { onClose: () => void } = $props();

  let filters = $state<Filter[]>([]);
  let loading = $state(false);
  let error = $state("");

  // Draft (used for both create and edit).
  type Draft = {
    id: number | null;
    name: string;
    field: FilterMatch["field"];
    op: FilterMatch["op"];
    value: string;
    case_sensitive: boolean;
    action: "mark_read" | "star" | "hide";
    enabled: boolean;
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
  });
  let draft = $state<Draft>(emptyDraft());

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
    };
  }

  async function save() {
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
      <button class="close" on:click={onClose} aria-label="Close">×</button>
    </header>

    {#if error}
      <p class="error" data-testid="filter-error">{error}</p>
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
          </select>
        </label>
        <label>
          <span>Op</span>
          <select bind:value={draft.op} data-testid="filter-op">
            <option value="contains">contains</option>
            <option value="equals">equals</option>
            <option value="starts_with">starts with</option>
            <option value="matches">matches (regex)</option>
          </select>
        </label>
      </div>
      <label>
        <span>Value</span>
        <input bind:value={draft.value} placeholder="crypto" data-testid="filter-value" />
      </label>
      <label class="checkbox">
        <input type="checkbox" bind:checked={draft.case_sensitive} />
        <span>Case sensitive</span>
      </label>
      <div class="row">
        <label>
          <span>Action</span>
          <select bind:value={draft.action} data-testid="filter-action">
            <option value="mark_read">Mark read</option>
            <option value="star">Star</option>
            <option value="hide">Hide (mark read)</option>
          </select>
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
  .error {
    background: #fef2f2;
    color: #991b1b;
    border-radius: 4px;
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
  }
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
  .actions {
    margin-top: 0.5rem;
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  button {
    background: var(--accent);
    color: white;
    border: 0;
    padding: 0.4rem 0.85rem;
    border-radius: 4px;
    cursor: pointer;
    font: inherit;
  }
  button.ghost {
    background: transparent;
    color: inherit;
    border: 1px solid var(--border);
  }
  button.ghost.danger { color: #b91c1c; border-color: #fecaca; }
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
