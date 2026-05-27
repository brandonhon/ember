<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import {
    user,
    logout,
    theme,
    refreshSidebar,
    activeView,
    loadArticles,
    sidebarCollapsed,
    branding,
  } from "../lib/stores";
  import { api, ApiError } from "../lib/api";
  import { get } from "svelte/store";

  let {
    onOpenSettings,
    mobile = false,
    onToggleMobileSidebar,
    showBack = false,
    onBack,
  }: {
    onOpenSettings: () => void;
    mobile?: boolean;
    onToggleMobileSidebar?: () => void;
    showBack?: boolean;
    onBack?: () => void;
  } = $props();

  let searchQ = $state("");
  let searchResults = $state<{ id: number; title: string; url?: string }[]>([]);
  let showResults = $state(false);
  let popoverOpen = $state(false);
  let opmlInput: HTMLInputElement | undefined = $state();
  let importMsg = $state("");
  let polling = $state(false);

  // Close the popover and result dropdown on outside click.
  function onDocClick(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (popoverOpen && !target.closest("[data-popover]") && !target.closest("[data-user-chip]")) {
      popoverOpen = false;
    }
    if (
      showResults &&
      !target.closest("[data-search-results]") &&
      !target.closest("[data-search-form]")
    ) {
      showResults = false;
    }
  }
  onMount(() => document.addEventListener("click", onDocClick));
  onDestroy(() => document.removeEventListener("click", onDocClick));

  async function onSearch(e: Event) {
    e.preventDefault();
    const q = searchQ.trim();
    if (!q) {
      searchResults = [];
      showResults = false;
      return;
    }
    // Submit -> dedicated search view in the list pane. The dropdown is
    // kept as a "did you mean this single hit" preview when the user is
    // still typing; pressing Enter switches to the full results view.
    activeView.set({ kind: "search", query: q });
    void loadArticles({ kind: "search", query: q });
    showResults = false;
  }

  // Clear the search input, the dropdown, and the search view in one click.
  // Snaps back to Fresh so the user always lands somewhere sensible.
  function clearSearch() {
    searchQ = "";
    searchResults = [];
    showResults = false;
    if (get(activeView).kind === "search") {
      const fresh = { kind: "smart" as const, view: "fresh" as const };
      activeView.set(fresh);
      void loadArticles(fresh);
    }
  }

  // Live typeahead preview (top-3 hits) shown under the input until the
  // user submits.
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  function onSearchInput() {
    if (searchTimer) clearTimeout(searchTimer);
    const q = searchQ.trim();
    if (!q) {
      searchResults = [];
      showResults = false;
      return;
    }
    searchTimer = setTimeout(async () => {
      try {
        const res = await api.search(q, 6);
        searchResults = (res.data ?? []).map((r) => ({ id: r.id, title: r.title, url: r.url }));
        showResults = searchResults.length > 0;
      } catch {
        searchResults = [];
      }
    }, 220);
  }

  function toggleTheme() {
    theme.update((t) => (t === "light" ? "dark" : "light"));
  }

  async function refreshAll() {
    if (polling) return;
    polling = true;
    try {
      await refreshSidebar();
      await loadArticles(get(activeView));
    } finally {
      polling = false;
    }
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
    window.location.href = "/api/feeds/export";
  }

  function initials(name: string): string {
    return (name?.[0] ?? "?").toUpperCase();
  }
</script>

<header class="topbar" class:mobile>
  {#if mobile && showBack}
    <button class="mobile-icon-btn" on:click={() => onBack?.()} aria-label="Back to article list" data-testid="mobile-back">
      <svg viewBox="0 0 24 24" width="22" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M15 18l-6-6 6-6"/></svg>
    </button>
  {:else if mobile}
    <button class="mobile-icon-btn" on:click={() => onToggleMobileSidebar?.()} aria-label="Open sidebar" data-testid="mobile-menu">
      <svg viewBox="0 0 24 24" width="22" fill="none" stroke="currentColor" stroke-width="2.2"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>
    </button>
  {/if}
  <div class="brand">
    <span class="kite" aria-hidden="true">
      <svg viewBox="0 0 64 64">
        <defs>
          <linearGradient id="brand-emb" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stop-color="var(--ember-soft)" />
            <stop offset="1" stop-color="var(--ember)" />
          </linearGradient>
        </defs>
        <circle cx="13" cy="15" r="6.5" fill="url(#brand-emb)" />
        <rect x="25" y="11.5" width="31" height="8" rx="4" fill="var(--ink)" />
        <rect x="8" y="28" width="48" height="8" rx="4" fill="var(--ink)" />
        <rect x="8" y="44.5" width="34" height="8" rx="4" fill="url(#brand-emb)" />
      </svg>
    </span>
    {$branding.name}
  </div>

  <form class="search" on:submit={onSearch} data-search-form>
    <svg viewBox="0 0 24 24" width="17" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="M21 21l-4-4" />
    </svg>
    <input
      bind:value={searchQ}
      on:input={onSearchInput}
      on:keydown={(e) => { if (e.key === "Escape") clearSearch(); }}
      placeholder={mobile ? "Search…" : "Search all articles, sources, and notes…"}
      data-testid="search-input"
    />
    {#if searchQ}
      <button
        type="button"
        class="search-clear"
        on:click={clearSearch}
        aria-label="Clear search"
        title="Clear search (Esc)"
        data-testid="search-clear"
      >
        ×
      </button>
    {:else}
      <span class="kbd">/</span>
    {/if}
  </form>

  <div class="topbar-actions">
    <input
      type="file"
      accept=".opml,.xml,application/xml,text/xml"
      bind:this={opmlInput}
      on:change={onOPMLPick}
      style="display:none"
      data-testid="opml-input"
    />
    <button
      class="icon-btn"
      title={$sidebarCollapsed ? "Show sidebar" : "Hide sidebar"}
      on:click={() => {
        sidebarCollapsed.update((v) => !v);
        try { localStorage.setItem("ember:sidebar", $sidebarCollapsed ? "closed" : "open"); } catch {}
      }}
      data-testid="toggle-sidebar"
    >
      {#if $sidebarCollapsed}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <path d="M9 4v16" />
          <path d="M14 12l3-3M14 12l3 3" />
        </svg>
      {:else}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <path d="M9 4v16" />
        </svg>
      {/if}
    </button>
    <button
      class="icon-btn"
      title="Refresh feeds now"
      on:click={refreshAll}
      class:spinning={polling}
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
        <path d="M21 3v5h-5" />
      </svg>
    </button>
    <button class="icon-btn" title="Toggle theme" on:click={toggleTheme}>
      {#if $theme === "dark"}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" />
        </svg>
      {:else}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" />
        </svg>
      {/if}
    </button>
    <button
      class="user-chip"
      on:click={() => (popoverOpen = !popoverOpen)}
      data-user-chip
    >
      {#if $user}
        <span class="avatar">{initials($user.username)}</span>
        <span class="who">
          {$user.username}<small>{$user.is_admin ? "Administrator" : "Reader"}</small>
        </span>
      {/if}
    </button>
  </div>

  {#if importMsg}
    <p class="import-msg" data-testid="opml-msg">{importMsg}</p>
  {/if}

  {#if showResults && searchResults.length > 0}
    <div class="search-results" data-search-results data-testid="search-results">
      {#each searchResults as r (r.id)}
        <a href={r.url || "#"} target="_blank" rel="noopener noreferrer">{r.title}</a>
      {/each}
    </div>
  {/if}

  {#if popoverOpen}
    <div class="popover" data-popover role="menu">
      <div class="pop-user">
        <span class="avatar">{initials($user?.username ?? "")}</span>
        <div>
          <div class="pop-name">{$user?.username}</div>
          <div class="pop-mail">{$user?.email || "—"}</div>
        </div>
      </div>
      <button
        class="pop-item"
        on:click={() => {
          popoverOpen = false;
          onOpenSettings();
        }}
        data-testid="open-settings"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .4 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.4 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.6 1.7 1.7 0 0 0-1.9.4l-.1.1A2 2 0 1 1 4.2 17l.1-.1a1.7 1.7 0 0 0 .4-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.6-1.1 1.7 1.7 0 0 0-.4-1.9l-.1-.1A2 2 0 1 1 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.4H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.4l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.4 1.9V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z" /></svg>
        Settings
      </button>
      <button
        class="pop-item"
        on:click={() => {
          popoverOpen = false;
          opmlInput?.click();
        }}
        data-testid="open-opml-import"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><path d="M7 10l5 5 5-5" /><path d="M12 15V3" /></svg>
        Import OPML
      </button>
      <button class="pop-item" on:click={() => { popoverOpen = false; downloadOPML(); }}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><path d="M7 10l5-5 5 5" /><path d="M12 5v12" /></svg>
        Export OPML
      </button>
      <div class="pop-sep"></div>
      <button class="pop-item" on:click={() => { popoverOpen = false; logout(); }} data-testid="logout">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" /></svg>
        Sign out
      </button>
    </div>
  {/if}
</header>

<style>
  .topbar {
    position: relative;
    display: grid;
    grid-template-columns: var(--rail-w) 1fr auto;
    align-items: center;
    border-bottom: 1px solid var(--line);
    background: var(--paper-2);
    /* No gap between columns: the search column starts exactly at the
       sidebar's right edge (var(--rail-w)), so the search input's left
       border lines up with the rail. Mobile keeps its own gap rule below.
       Actions sit at the right edge via the 1fr search column absorbing
       leftover space, so removing the gap doesn't visually crowd them. */
    gap: 0;
    padding-right: 18px;
  }
  .topbar.mobile {
    /* Three slots: nav icon, brand (collapses), search. The brand text gets
       hidden entirely below ~520px so the search input gets enough room to
       show a real placeholder. */
    grid-template-columns: auto auto 1fr;
    gap: 6px;
    padding-right: 8px;
  }
  .topbar.mobile .brand {
    padding-left: 0;
    font-size: 16px;
  }
  .topbar.mobile .brand .kite { display: none; }
  .topbar.mobile .search {
    max-width: none;
    padding: 6px 10px;
    gap: 6px;
  }
  .topbar.mobile .search input {
    /* Show a short placeholder on mobile so the bar doesn't look like a
       blank slot. */
    font-size: 13px;
  }
  .topbar.mobile .search .kbd { display: none; }
  @media (max-width: 520px) {
    .topbar.mobile .brand { display: none; }
  }
  .mobile-icon-btn {
    background: transparent;
    border: 0;
    padding: 8px 10px;
    color: var(--ink);
    cursor: pointer;
    display: grid;
    place-items: center;
    margin-left: 6px;
    border-radius: 8px;
  }
  .mobile-icon-btn:hover { background: var(--line-soft); }
  /* Hide some desktop-only action buttons on mobile via class on parent. */
  .topbar.mobile .desktop-only { display: none; }
  .brand {
    display: flex;
    align-items: center;
    gap: 4px;
    padding-left: 22px;
    font-family: var(--font-display);
    font-weight: 600;
    font-size: 18px;
    letter-spacing: -0.01em;
  }
  .brand .kite {
    width: 24px;
    height: 24px;
    display: grid;
    place-items: center;
  }
  .brand .kite svg { width: 24px; height: 24px; }

  .search {
    display: flex;
    align-items: center;
    gap: 10px;
    max-width: 520px;
    width: 100%;
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 11px;
    padding: 8px 13px;
    color: var(--ink-faint);
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .search:focus-within {
    border-color: var(--ember);
    box-shadow: 0 0 0 3px var(--ember-wash);
  }
  .search input {
    border: none;
    background: none;
    outline: none;
    width: 100%;
    font-family: var(--font-ui);
    font-size: 13px;
    color: var(--ink);
  }
  .search input::placeholder { color: var(--ink-faint); }
  .search .kbd {
    font-size: 10px;
    border: 1px solid var(--line);
    border-radius: 5px;
    padding: 1px 6px;
    color: var(--ink-faint);
  }
  .search-clear {
    background: transparent;
    border: 0;
    color: var(--ink-faint);
    cursor: pointer;
    font-size: 18px;
    line-height: 1;
    padding: 2px 6px;
    border-radius: 6px;
  }
  .search-clear:hover { background: var(--line-soft); color: var(--ember); }

  .topbar-actions { display: flex; align-items: center; gap: 6px; }
  .icon-btn {
    width: 36px;
    height: 36px;
    border-radius: 10px;
    display: grid;
    place-items: center;
    color: var(--ink-soft);
    transition: background 0.15s, color 0.15s;
  }
  .icon-btn:hover { background: var(--line-soft); color: var(--ink); }
  .icon-btn svg { width: 18px; height: 18px; }
  .icon-btn.spinning svg { animation: spin 0.9s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  .user-chip {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 4px 10px 4px 4px;
    border-radius: 30px;
    border: 1px solid var(--line);
    background: var(--card);
    transition: border-color 0.15s;
  }
  .user-chip:hover { border-color: var(--ink-faint); }
  .avatar {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: var(--ember);
    color: #fff;
    display: grid;
    place-items: center;
    font-weight: 700;
    font-size: 12px;
  }
  .who { font-size: 12px; line-height: 1.1; }
  .who small { display: block; color: var(--ink-faint); font-size: 10.5px; }

  .popover {
    position: absolute;
    top: calc(var(--topbar-h) - 4px);
    right: 18px;
    /* Above ShortcutHelp / Settings / WelcomeModal backdrops (z-index 100)
       so the user can always reach Settings from the chip menu, even if
       another modal is somehow open. */
    z-index: 200;
    width: 248px;
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 14px;
    box-shadow: var(--shadow-pane);
    padding: 8px;
  }
  .pop-user {
    padding: 10px;
    display: flex;
    align-items: center;
    gap: 11px;
    border-bottom: 1px solid var(--line-soft);
    margin-bottom: 6px;
  }
  .pop-user .avatar { width: 36px; height: 36px; font-size: 14px; }
  .pop-name { font-weight: 600; font-size: 13px; }
  .pop-mail { font-size: 11px; color: var(--ink-faint); }
  .pop-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    text-align: left;
    padding: 8px 10px;
    border-radius: 9px;
    font-size: 13px;
    color: var(--ink-soft);
  }
  .pop-item:hover { background: var(--line-soft); color: var(--ink); }
  .pop-item svg { width: 15px; height: 15px; }
  .pop-sep { height: 1px; background: var(--line-soft); margin: 6px 0; }

  .search-results {
    position: absolute;
    top: calc(var(--topbar-h) - 4px);
    left: calc(var(--rail-w) + 16px);
    background: var(--card);
    border: 1px solid var(--line);
    box-shadow: var(--shadow-pane);
    padding: 6px;
    max-height: 320px;
    overflow-y: auto;
    z-index: 60;
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 420px;
    border-radius: 11px;
  }
  .search-results a {
    color: var(--ink);
    text-decoration: none;
    font-size: 13px;
    padding: 8px 10px;
    border-radius: 8px;
    line-height: 1.4;
  }
  .search-results a:hover { background: var(--line-soft); }

  .import-msg {
    position: absolute;
    top: calc(var(--topbar-h) + 4px);
    right: 18px;
    background: var(--ember-wash);
    color: var(--ember);
    padding: 6px 12px;
    border-radius: 8px;
    font-size: 12px;
    margin: 0;
    border: 1px solid var(--ember);
    z-index: 10;
  }
</style>
