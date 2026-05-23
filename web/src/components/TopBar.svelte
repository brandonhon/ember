<script lang="ts">
  import { user, logout, theme, refreshSidebar } from "../lib/stores";
  import { api, ApiError } from "../lib/api";
  import FilterManager from "./FilterManager.svelte";
  import ManageUsers from "./ManageUsers.svelte";

  let newFeedURL = $state("");
  let searchQ = $state("");
  let searchResults = $state<{ id: number; title: string; url?: string }[]>([]);
  let busy = $state(false);
  let showFilters = $state(false);
  let showUsers = $state(false);
  let opmlInput: HTMLInputElement | undefined = $state();
  let importMsg = $state("");

  async function onAdd(e: Event) {
    e.preventDefault();
    if (!newFeedURL.trim()) return;
    busy = true;
    try {
      await api.addFeed(newFeedURL.trim());
      newFeedURL = "";
      await refreshSidebar();
    } finally {
      busy = false;
    }
  }

  async function onSearch(e: Event) {
    e.preventDefault();
    if (!searchQ.trim()) {
      searchResults = [];
      return;
    }
    try {
      const res = await api.search(searchQ.trim());
      searchResults = res.data.map((r) => ({ id: r.id, title: r.title, url: r.url }));
    } catch {
      searchResults = [];
    }
  }

  function toggleTheme() {
    theme.update((t) => (t === "light" ? "dark" : "light"));
  }

  async function onOPMLPick(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importMsg = "Importing…";
    try {
      const res = await api.importOPML(file);
      importMsg = `Imported ${res.data.imported} subscriptions`;
      await refreshSidebar();
    } catch (err) {
      importMsg = err instanceof ApiError ? err.message : String(err);
    } finally {
      input.value = "";
      setTimeout(() => (importMsg = ""), 4000);
    }
  }

  function downloadOPML() {
    // Use the export endpoint directly so the browser triggers the
    // attachment download per the server's Content-Disposition header.
    window.location.href = "/api/feeds/export";
  }
</script>

<header class="topbar">
  <div class="brand">ember</div>
  <form class="add" on:submit={onAdd}>
    <input
      type="url"
      bind:value={newFeedURL}
      placeholder="Add feed URL"
      disabled={busy}
      data-testid="add-feed-input"
    />
    <button type="submit" disabled={busy || !newFeedURL.trim()} data-testid="add-feed-submit">
      Add
    </button>
  </form>
  <form class="search" on:submit={onSearch}>
    <input
      type="search"
      bind:value={searchQ}
      placeholder="Search articles"
      data-testid="search-input"
    />
  </form>
  {#if searchResults.length > 0}
    <div class="search-results" data-testid="search-results">
      {#each searchResults as r (r.id)}
        <a href={r.url || "#"} target="_blank" rel="noopener noreferrer">{r.title}</a>
      {/each}
    </div>
  {/if}
  <div class="user-actions">
    <input
      type="file"
      accept=".opml,.xml,application/xml,text/xml"
      bind:this={opmlInput}
      on:change={onOPMLPick}
      style="display:none"
      data-testid="opml-input"
    />
    <button on:click={() => opmlInput?.click()} aria-label="Import OPML" data-testid="open-opml-import">
      Import OPML
    </button>
    <button on:click={downloadOPML} aria-label="Export OPML">
      Export
    </button>
    <button on:click={() => (showFilters = true)} aria-label="Manage filters" data-testid="open-filters">
      Filters
    </button>
    {#if $user?.is_admin}
      <button on:click={() => (showUsers = true)} aria-label="Manage users" data-testid="open-users">
        Users
      </button>
    {/if}
    <button on:click={toggleTheme} aria-label="Toggle theme">
      {$theme === "dark" ? "☀" : "☾"}
    </button>
    {#if $user}
      <span class="username">{$user.username}</span>
    {/if}
    <button on:click={logout} data-testid="logout">Sign out</button>
  </div>
  {#if importMsg}
    <p class="import-msg" data-testid="opml-msg">{importMsg}</p>
  {/if}
</header>

{#if showFilters}
  <FilterManager onClose={() => (showFilters = false)} />
{/if}
{#if showUsers}
  <ManageUsers onClose={() => (showUsers = false)} />
{/if}

<style>
  .topbar {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.5rem 1rem;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
    position: relative;
  }
  .brand { font-family: "Fraunces", serif; font-size: 1.2rem; }
  .add, .search { display: flex; gap: 0.25rem; }
  input {
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    font: inherit;
  }
  .add input { width: 240px; }
  .search input { width: 220px; }
  button {
    background: var(--accent);
    color: white;
    border: 0;
    padding: 0.35rem 0.7rem;
    border-radius: 4px;
    cursor: pointer;
    font: inherit;
  }
  button:disabled { opacity: 0.5; }
  .user-actions { margin-left: auto; display: flex; gap: 0.5rem; align-items: center; }
  .user-actions button {
    background: transparent;
    color: inherit;
    border: 1px solid var(--border);
  }
  .username { color: var(--muted); font-size: 0.85rem; }
  .search-results {
    position: absolute;
    top: 100%;
    right: 1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-top: 0;
    padding: 0.5rem;
    max-height: 260px;
    overflow-y: auto;
    z-index: 10;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    width: 300px;
  }
  .search-results a {
    color: inherit;
    text-decoration: none;
    font-size: 0.85rem;
    padding: 0.25rem;
    border-radius: 4px;
  }
  .search-results a:hover { background: var(--hover); }
  .import-msg {
    position: absolute;
    top: 100%;
    right: 1rem;
    background: var(--accent-bg);
    color: var(--accent);
    padding: 0.4rem 0.75rem;
    border-radius: 4px;
    font-size: 0.85rem;
    margin: 0.25rem 0 0;
    border: 1px solid var(--accent);
    z-index: 10;
  }
</style>
