<script lang="ts">
  import {
    activeView,
    categories,
    feeds,
    totalUnread,
    loadArticles,
    refreshSidebar,
  } from "../lib/stores";
  import { api, ApiError } from "../lib/api";
  import type { FeedWithCounts } from "../lib/types";

  let collapsedCategories = $state<Record<number, boolean>>({});
  let collapsedUncategorized = $state(false);
  let addFormOpen = $state(false);
  let addingFeed = $state(false);
  let newFeedURL = $state("");
  let addError = $state("");

  const grouped = $derived.by(() => {
    const byCat = new Map<number, FeedWithCounts[]>();
    const uncat: FeedWithCounts[] = [];
    for (const f of $feeds) {
      if (f.category_id) {
        const arr = byCat.get(f.category_id) ?? [];
        arr.push(f);
        byCat.set(f.category_id, arr);
      } else {
        uncat.push(f);
      }
    }
    return { byCat, uncat };
  });

  function unreadInCategory(catID: number): number {
    let sum = 0;
    const list = grouped.byCat.get(catID) ?? [];
    for (const f of list) sum += f.unread || 0;
    return sum;
  }

  // Deterministic dot color per category id.
  const DOT_COLORS = ["#3b82c4", "#4f7a3d", "#b07d1a", "#c2451d", "#7a3d8b", "#1d4ed8"];
  function dotColor(catID: number): string {
    return DOT_COLORS[catID % DOT_COLORS.length];
  }
  const FAV_COLORS = ["#ff6154", "#0a0a0a", "#e63946", "#1d4ed8", "#623ce6", "#ee0000", "#326ce5", "#111", "#cc0000", "#bb1919"];
  function favColor(feedID: number): string {
    return FAV_COLORS[feedID % FAV_COLORS.length];
  }
  function faviconLetter(title: string): string {
    return (title?.[0] ?? "?").toUpperCase();
  }

  function pickSmart(view: "fresh" | "today" | "unread" | "starred" | "later" | "shared") {
    activeView.set({ kind: "smart", view });
    loadArticles({ kind: "smart", view });
  }
  function pickFeed(id: number) {
    activeView.set({ kind: "feed", id });
    loadArticles({ kind: "feed", id });
  }
  function pickCategory(id: number) {
    activeView.set({ kind: "category", id });
    loadArticles({ kind: "category", id });
  }
  function toggleCategory(catID: number) {
    collapsedCategories[catID] = !collapsedCategories[catID];
  }

  async function submitAddFeed(e: Event) {
    e.preventDefault();
    if (!newFeedURL.trim()) return;
    addError = "";
    addingFeed = true;
    try {
      await api.addFeed(newFeedURL.trim());
      newFeedURL = "";
      addFormOpen = false;
      await refreshSidebar();
    } catch (err) {
      addError = err instanceof ApiError ? err.message : String(err);
    } finally {
      addingFeed = false;
    }
  }

  function cancelAdd() {
    addFormOpen = false;
    newFeedURL = "";
    addError = "";
  }

  function isActiveSmart(v: string): boolean {
    return $activeView.kind === "smart" && $activeView.view === v;
  }
  function isActiveFeed(id: number): boolean {
    return $activeView.kind === "feed" && $activeView.id === id;
  }
  function isActiveCategory(id: number): boolean {
    return $activeView.kind === "category" && $activeView.id === id;
  }
</script>

<aside class="rail">
  <!-- Smart views -->
  <div class="rail-section">
    <button class="nav-item" class:active={isActiveSmart("today")} on:click={() => pickSmart("today")}>
      <span class="ni-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>
      </span>
      <span class="ni-label">Today</span>
    </button>
    <button class="nav-item" class:active={isActiveSmart("fresh")} on:click={() => pickSmart("fresh")} data-testid="view-fresh">
      <span class="ni-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9z" /></svg>
      </span>
      <span class="ni-label">Fresh</span>
    </button>
    <button class="nav-item" class:active={isActiveSmart("unread")} on:click={() => pickSmart("unread")}>
      <span class="ni-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9" /></svg>
      </span>
      <span class="ni-label">All Unread</span>
      {#if $totalUnread > 0}<span class="badge">{$totalUnread}</span>{/if}
    </button>
    <button class="nav-item" class:active={isActiveSmart("starred")} on:click={() => pickSmart("starred")}>
      <span class="ni-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2l3 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.9 21l1.2-6.8-5-4.9 6.9-1z" /></svg>
      </span>
      <span class="ni-label">Starred</span>
    </button>
    <button class="nav-item" class:active={isActiveSmart("later")} on:click={() => pickSmart("later")}>
      <span class="ni-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" /></svg>
      </span>
      <span class="ni-label">Read Later</span>
    </button>
    <button class="nav-item" class:active={isActiveSmart("shared")} on:click={() => pickSmart("shared")}>
      <span class="ni-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="18" cy="5" r="3" /><circle cx="6" cy="12" r="3" /><circle cx="18" cy="19" r="3" /><path d="M8.6 13.5l6.8 4M15.4 6.5l-6.8 4" /></svg>
      </span>
      <span class="ni-label">Shared with me</span>
    </button>
  </div>

  <!-- Folders + feeds -->
  <div class="rail-section">
    <div class="rail-head"><h3>Folders</h3></div>

    {#each $categories as cat (cat.id)}
      <div class="folder" class:collapsed={collapsedCategories[cat.id]}>
        <div class="folder-head">
          <button class="chev-btn" on:click={() => toggleCategory(cat.id)} aria-label="Toggle folder">
            <svg class="chev" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M6 9l6 6 6-6" /></svg>
          </button>
          <span class="dot" style="background:{cat.color || dotColor(cat.id)}"></span>
          <button
            class="folder-name"
            class:active={isActiveCategory(cat.id)}
            on:click={() => pickCategory(cat.id)}
          >
            {cat.name}
          </button>
          {#if unreadInCategory(cat.id) > 0}
            <span class="badge">{unreadInCategory(cat.id)}</span>
          {/if}
        </div>
        <div class="feed-list">
          {#each grouped.byCat.get(cat.id) ?? [] as f (f.id)}
            <button
              class="feed-item"
              class:active={isActiveFeed(f.id)}
              class:read={f.unread === 0}
              on:click={() => pickFeed(f.id)}
              data-testid="feed-{f.id}"
            >
              <span class="favicon" style="background:{favColor(f.id)}">{faviconLetter(f.title_override || f.title)}</span>
              <span class="ni-label">{f.title_override || f.title}</span>
              {#if f.unread > 0}<span class="badge">{f.unread}</span>{/if}
            </button>
          {/each}
        </div>
      </div>
    {/each}

    {#if grouped.uncat.length > 0}
      <div class="folder" class:collapsed={collapsedUncategorized}>
        <div class="folder-head">
          <button class="chev-btn" on:click={() => (collapsedUncategorized = !collapsedUncategorized)} aria-label="Toggle folder">
            <svg class="chev" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M6 9l6 6 6-6" /></svg>
          </button>
          <span class="dot" style="background:#8c8273"></span>
          <span class="folder-name">Uncategorized</span>
        </div>
        <div class="feed-list">
          {#each grouped.uncat as f (f.id)}
            <button
              class="feed-item"
              class:active={isActiveFeed(f.id)}
              class:read={f.unread === 0}
              on:click={() => pickFeed(f.id)}
              data-testid="feed-{f.id}"
            >
              <span class="favicon" style="background:{favColor(f.id)}">{faviconLetter(f.title_override || f.title)}</span>
              <span class="ni-label">{f.title_override || f.title}</span>
              {#if f.unread > 0}<span class="badge">{f.unread}</span>{/if}
            </button>
          {/each}
        </div>
      </div>
    {/if}

    <div class="add-row">
      {#if !addFormOpen}
        <button class="add-btn" on:click={() => (addFormOpen = true)} data-testid="open-add-feed">
          <svg viewBox="0 0 24 24" width="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14" /></svg>
          Add feed
        </button>
      {:else}
        <form class="add-form" on:submit={submitAddFeed}>
          <input
            type="url"
            bind:value={newFeedURL}
            placeholder="https://example.com/feed.xml"
            disabled={addingFeed}
            data-testid="add-feed-input"
          />
          <div class="add-form-actions">
            <button type="button" class="ghost" on:click={cancelAdd}>Cancel</button>
            <button type="submit" disabled={addingFeed || !newFeedURL.trim()} data-testid="add-feed-submit">
              {addingFeed ? "Adding…" : "Add"}
            </button>
          </div>
          {#if addError}<p class="add-error">{addError}</p>{/if}
        </form>
      {/if}
    </div>
  </div>
</aside>

<style>
  .rail {
    border-right: 1px solid var(--line);
    background: var(--paper-2);
    overflow-y: auto;
    padding: 14px 12px 40px;
  }
  .rail-section { margin-bottom: 22px; }
  .rail-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 10px 8px;
  }
  .rail-head h3 {
    font-size: 10.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--ink-faint);
    font-weight: 700;
    margin: 0;
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 11px;
    width: 100%;
    text-align: left;
    padding: 7px 10px;
    border-radius: 9px;
    color: var(--ink-soft);
    font-size: 13.5px;
    font-weight: 500;
    transition: background 0.12s, color 0.12s;
  }
  .nav-item:hover { background: var(--line-soft); color: var(--ink); }
  .nav-item.active { background: var(--ember-wash); color: var(--ember); }
  :global([data-theme="dark"]) .nav-item.active { color: var(--ember-soft); }
  .nav-item.active .ni-icon { color: var(--ember); }
  .ni-icon {
    width: 18px;
    height: 18px;
    flex: none;
    color: var(--ink-faint);
    display: grid;
    place-items: center;
  }
  .ni-icon svg { width: 16px; height: 16px; }
  .ni-label {
    flex: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .badge {
    font-size: 10.5px;
    font-weight: 700;
    color: var(--ink-faint);
    background: var(--line);
    padding: 1px 6px;
    border-radius: 20px;
    min-width: 20px;
    text-align: center;
  }
  .nav-item.active .badge { background: var(--ember); color: #fff; }

  .folder { margin-bottom: 2px; }
  .folder-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-radius: 9px;
    transition: background 0.12s;
  }
  .folder-head:hover { background: var(--line-soft); }
  .chev-btn {
    width: 14px;
    height: 14px;
    display: grid;
    place-items: center;
    transition: transform 0.18s;
    color: var(--ink-faint);
  }
  .chev { width: 14px; height: 14px; transition: transform 0.18s; }
  .folder.collapsed .chev { transform: rotate(-90deg); }
  .folder.collapsed .feed-list { display: none; }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 3px;
    flex: none;
  }
  .folder-name {
    flex: 1;
    text-align: left;
    font-size: 12.5px;
    font-weight: 600;
    color: var(--ink);
  }
  .folder-name.active { color: var(--ember); }

  .feed-list { padding-left: 14px; margin: 2px 0 6px; }
  .feed-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    text-align: left;
    padding: 5px 10px;
    border-radius: 8px;
    color: var(--ink-soft);
    font-size: 12.5px;
    transition: background 0.12s, color 0.12s;
  }
  .feed-item:hover { background: var(--line-soft); color: var(--ink); }
  .feed-item.active { background: var(--ember-wash); color: var(--ember); }
  .feed-item.read .ni-label { color: var(--ink-faint); }
  .feed-item.active .badge { background: var(--ember); color: #fff; }
  .feed-item .badge { background: transparent; }
  .favicon {
    width: 18px;
    height: 18px;
    border-radius: 5px;
    flex: none;
    display: grid;
    place-items: center;
    font-size: 9.5px;
    font-weight: 800;
    color: #fff;
  }

  .add-row { margin-top: 6px; padding: 0 4px; }
  .add-btn {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 8px 10px;
    border-radius: 9px;
    border: 1px dashed var(--line);
    color: var(--ink-faint);
    font-size: 12.5px;
    font-weight: 600;
    transition: all 0.12s;
    background: transparent;
  }
  .add-btn:hover {
    border-color: var(--ember);
    color: var(--ember);
    background: var(--ember-wash);
  }
  .add-form { display: flex; flex-direction: column; gap: 6px; }
  .add-form input {
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 12.5px;
    background: var(--card);
    color: var(--ink);
  }
  .add-form input:focus { outline: none; border-color: var(--ember); }
  .add-form-actions { display: flex; gap: 6px; justify-content: flex-end; }
  .add-form-actions button {
    padding: 5px 11px;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 600;
    background: var(--ember);
    color: #fff;
    border: none;
  }
  .add-form-actions button.ghost {
    background: transparent;
    color: var(--ink-soft);
    border: 1px solid var(--line);
  }
  .add-form-actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  .add-error { color: #b91c1c; font-size: 11px; margin: 0; }
</style>
