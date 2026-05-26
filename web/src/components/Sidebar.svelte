<script lang="ts">
  import {
    activeView,
    boards,
    categories,
    feeds,
    totalUnread,
    smartCounts,
    loadArticles,
    refreshSidebar,
    savedSearches,
  } from "../lib/stores";
  import { api, ApiError } from "../lib/api";
  import type { FeedWithCounts } from "../lib/types";
  import ConfirmDialog from "./ConfirmDialog.svelte";

  import { onMount, onDestroy } from "svelte";

  // Confirmation modal state. Set to an object to open the dialog; the
  // onConfirm callback runs the destructive action, and we clear state when
  // it returns. Replaces window.confirm().
  type ConfirmReq = {
    title?: string;
    message: string;
    confirmLabel?: string;
    destructive?: boolean;
    run: () => Promise<void> | void;
  };
  let confirmReq = $state<ConfirmReq | null>(null);
  let confirmBusy = $state(false);
  async function runConfirm() {
    if (!confirmReq) return;
    confirmBusy = true;
    try {
      await confirmReq.run();
      confirmReq = null;
    } finally {
      confirmBusy = false;
    }
  }

  let collapsedCategories = $state<Record<number, boolean>>({});
  let collapsedUncategorized = $state(false);
  let addFormOpen = $state(false);
  let addingFeed = $state(false);
  let newFeedURL = $state("");
  let addError = $state("");
  let newBoardName = $state("");
  let newBoardFormOpen = $state(false);
  let creatingBoard = $state(false);
  // Which feed row's action menu is open (by feed id). Null = none.
  let menuFor = $state<number | null>(null);
  // Per-category UI state: open menu, inline rename input, color popover.
  let categoryMenuFor = $state<number | null>(null);
  let renamingCategoryID = $state<number | null>(null);
  let renameValue = $state("");
  let colorPickerFor = $state<number | null>(null);

  const CAT_PALETTE = ["#3b82c4", "#4f7a3d", "#b07d1a", "#a93b16", "#7a3d8b", "#1d4ed8", "#7c4a2a", "#5b6770"];

  function onDocClick(e: MouseEvent) {
    const t = e.target as HTMLElement;
    if (menuFor !== null && !t.closest(`[data-feed-menu-for]`) && !t.closest(`[data-feed-actions-trigger]`)) {
      menuFor = null;
    }
    if (
      categoryMenuFor !== null &&
      !t.closest(`[data-cat-menu-for]`) &&
      !t.closest(`[data-cat-actions-trigger]`) &&
      !t.closest(`[data-cat-color-for]`)
    ) {
      categoryMenuFor = null;
      colorPickerFor = null;
    }
  }
  onMount(() => document.addEventListener("click", onDocClick));
  onDestroy(() => document.removeEventListener("click", onDocClick));

  async function toggleMute(f: FeedWithCounts) {
    menuFor = null;
    try {
      await api.updateFeed(f.subscription_id, { muted: !f.muted });
      await refreshSidebar();
    } catch (err) {
      console.error("toggleMute", err);
    }
  }

  async function deleteFeed(f: FeedWithCounts) {
    menuFor = null;
    const name = f.title_override || f.title;
    confirmReq = {
      title: "Unsubscribe?",
      message: `Remove "${name}" from your list. The feed itself stays available for other users.`,
      confirmLabel: "Unsubscribe",
      destructive: true,
      run: () => doDeleteFeed(f),
    };
  }

  async function doDeleteFeed(f: FeedWithCounts) {
    try {
      await api.deleteFeed(f.subscription_id);
      await refreshSidebar();
      // If the deleted feed was the active view, fall back to Fresh.
      if ($activeView.kind === "feed" && $activeView.id === f.id) {
        pickSmart("fresh");
      }
    } catch (err) {
      console.error("deleteFeed", err);
    }
  }

  async function resummarize(f: FeedWithCounts) {
    menuFor = null;
    try {
      const res = await api.resummarizeFeed(f.subscription_id);
      alert(`Re-enqueued ${res.data.enqueued} of ${res.data.reset} skipped articles for summarization.`);
      await refreshSidebar();
    } catch (err) {
      console.error("resummarize", err);
    }
  }

  async function markFeedRead(f: FeedWithCounts) {
    menuFor = null;
    try {
      await api.markAllRead({ feed_id: f.id });
      // Reload the current view's list and refresh the sidebar badges.
      await Promise.all([loadArticles($activeView), refreshSidebar()]);
    } catch (err) {
      console.error("markFeedRead", err);
    }
  }

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

  // Drag-and-drop reorder ----------------------------------------------
  // Folders and feeds can be reordered by dragging. State is local to the
  // sidebar; we mutate the categories/feeds stores optimistically on drop and
  // POST the new ordering to the server. A failure refreshes from server.
  type DragRef =
    | { kind: "folder"; id: number }
    | { kind: "feed"; id: number; cat: number | 0 };
  let drag = $state<DragRef | null>(null);
  let dropTarget = $state<DragRef | null>(null);

  function dragKey(r: DragRef): string {
    return r.kind === "folder" ? `folder:${r.id}` : `feed:${r.id}:${r.cat}`;
  }
  function sameDrag(a: DragRef | null, b: DragRef | null): boolean {
    if (!a || !b) return false;
    return dragKey(a) === dragKey(b);
  }

  function onFolderDragStart(e: DragEvent, catID: number) {
    drag = { kind: "folder", id: catID };
    e.dataTransfer?.setData("text/x-ember", dragKey(drag));
    if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
  }
  function onFeedDragStart(e: DragEvent, f: FeedWithCounts) {
    drag = { kind: "feed", id: f.subscription_id, cat: f.category_id ?? 0 };
    e.dataTransfer?.setData("text/x-ember", dragKey(drag));
    if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
  }
  function onDragOver(e: DragEvent, target: DragRef) {
    if (!drag || drag.kind !== target.kind) return;
    if (drag.kind === "feed" && target.kind === "feed" && drag.cat !== target.cat) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    dropTarget = target;
  }
  function onDragEnd() {
    drag = null;
    dropTarget = null;
  }
  async function onFolderDrop(e: DragEvent, targetID: number) {
    e.preventDefault();
    if (!drag || drag.kind !== "folder" || drag.id === targetID) {
      onDragEnd();
      return;
    }
    const ids = $categories.map((c) => c.id);
    const from = ids.indexOf(drag.id);
    const to = ids.indexOf(targetID);
    if (from < 0 || to < 0) {
      onDragEnd();
      return;
    }
    const [moved] = ids.splice(from, 1);
    ids.splice(to, 0, moved);
    categories.update((cs) => {
      const map = new Map(cs.map((c) => [c.id, c] as const));
      return ids.map((id, i) => ({ ...(map.get(id) as typeof cs[number]), position: i }));
    });
    onDragEnd();
    try {
      await api.reorderCategories(ids);
    } catch (err) {
      console.error("reorderCategories", err);
      await refreshSidebar();
    }
  }
  async function onFeedDrop(e: DragEvent, target: FeedWithCounts) {
    e.preventDefault();
    if (!drag || drag.kind !== "feed") {
      onDragEnd();
      return;
    }
    if (drag.id === target.subscription_id) {
      onDragEnd();
      return;
    }
    const sameCat = drag.cat === (target.category_id ?? 0);
    if (!sameCat) {
      onDragEnd();
      return;
    }
    // Reorder within the affected category. Server takes ids in display order;
    // we only send the affected slice (feeds in this category), since
    // ReorderSubscriptions only updates the rows it sees.
    const list = (target.category_id
      ? grouped.byCat.get(target.category_id) ?? []
      : grouped.uncat).slice();
    const ids = list.map((f) => f.subscription_id);
    const from = ids.indexOf(drag.id);
    const to = ids.indexOf(target.subscription_id);
    if (from < 0 || to < 0) {
      onDragEnd();
      return;
    }
    const [moved] = ids.splice(from, 1);
    ids.splice(to, 0, moved);
    // Mutate the feeds store: reorder within this category.
    feeds.update((fs) => {
      const sub2pos = new Map(ids.map((id, i) => [id, i] as const));
      return fs.slice().sort((a, b) => {
        const ap = sub2pos.get(a.subscription_id);
        const bp = sub2pos.get(b.subscription_id);
        if (ap !== undefined && bp !== undefined) return ap - bp;
        if (ap !== undefined) return -1;
        if (bp !== undefined) return 1;
        return 0;
      });
    });
    onDragEnd();
    try {
      await api.reorderFeeds(ids);
    } catch (err) {
      console.error("reorderFeeds", err);
      await refreshSidebar();
    }
  }

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
  function pickBoard(id: number) {
    activeView.set({ kind: "board", id });
    loadArticles({ kind: "board", id });
  }
  function pickSavedSearch(id: number, query: string) {
    const v = { kind: "search" as const, query, savedID: id };
    activeView.set(v);
    loadArticles(v);
  }
  // New saved search inline form state.
  let newSearchFormOpen = $state(false);
  let newSearchName = $state("");
  let newSearchQuery = $state("");
  let creatingSearch = $state(false);
  async function submitNewSavedSearch(e: Event) {
    e.preventDefault();
    if (!newSearchName.trim() || !newSearchQuery.trim()) return;
    creatingSearch = true;
    try {
      await api.createSavedSearch(newSearchName.trim(), newSearchQuery.trim());
      newSearchName = "";
      newSearchQuery = "";
      newSearchFormOpen = false;
      await refreshSidebar();
    } catch (err) {
      console.error("createSavedSearch", err);
    } finally {
      creatingSearch = false;
    }
  }
  function deleteSavedSearch(id: number, name: string) {
    confirmReq = {
      title: "Delete saved search?",
      message: `Remove "${name}" from your saved searches.`,
      confirmLabel: "Delete",
      destructive: true,
      run: async () => {
        try {
          await api.deleteSavedSearch(id);
          await refreshSidebar();
        } catch (err) {
          console.error("deleteSavedSearch", err);
        }
      },
    };
  }
  function isActiveSavedSearch(id: number): boolean {
    return $activeView.kind === "search" && $activeView.savedID === id;
  }
  function toggleCategory(catID: number) {
    collapsedCategories[catID] = !collapsedCategories[catID];
  }

  function startRenameCategory(catID: number, current: string) {
    categoryMenuFor = null;
    renamingCategoryID = catID;
    renameValue = current;
  }

  async function commitRename(catID: number) {
    const name = renameValue.trim();
    renamingCategoryID = null;
    if (!name) return;
    try {
      await api.updateCategory(catID, { name });
      await refreshSidebar();
    } catch (err) {
      console.error("rename category", err);
    }
  }

  function cancelRename() {
    renamingCategoryID = null;
  }

  async function pickCategoryColor(catID: number, color: string) {
    colorPickerFor = null;
    categoryMenuFor = null;
    try {
      await api.updateCategory(catID, { color });
      await refreshSidebar();
    } catch (err) {
      console.error("color category", err);
    }
  }

  async function deleteCategory(catID: number, name: string) {
    categoryMenuFor = null;
    confirmReq = {
      title: "Delete folder?",
      message: `Delete "${name}". Feeds inside it become uncategorized.`,
      confirmLabel: "Delete",
      destructive: true,
      run: () => doDeleteCategory(catID),
    };
  }

  async function doDeleteCategory(catID: number) {
    try {
      await api.deleteCategory(catID);
      await refreshSidebar();
      if ($activeView.kind === "category" && $activeView.id === catID) {
        pickSmart("fresh");
      }
    } catch (err) {
      console.error("delete category", err);
    }
  }

  async function submitNewBoard(e: Event) {
    e.preventDefault();
    if (!newBoardName.trim()) return;
    creatingBoard = true;
    try {
      await api.createBoard(newBoardName.trim());
      newBoardName = "";
      newBoardFormOpen = false;
      await refreshSidebar();
    } catch (err) {
      console.error("createBoard", err);
    } finally {
      creatingBoard = false;
    }
  }

  async function deleteBoard(id: number, name: string) {
    confirmReq = {
      title: "Delete board?",
      message: `Delete the board "${name}". Articles inside it stay in your library.`,
      confirmLabel: "Delete",
      destructive: true,
      run: () => doDeleteBoard(id),
    };
  }

  async function doDeleteBoard(id: number) {
    try {
      await api.deleteBoard(id);
      await refreshSidebar();
      if ($activeView.kind === "board" && $activeView.id === id) {
        activeView.set({ kind: "smart", view: "fresh" });
        loadArticles({ kind: "smart", view: "fresh" });
      }
    } catch (err) {
      console.error("deleteBoard", err);
    }
  }

  function isActiveBoard(id: number): boolean {
    return $activeView.kind === "board" && $activeView.id === id;
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

{#snippet feedRow(f: FeedWithCounts)}
  <div
    class="feed-row"
    class:muted={f.muted}
    class:errored={(f.error_count ?? 0) >= 3}
    class:drop-target={sameDrag(dropTarget, { kind: "feed", id: f.subscription_id, cat: f.category_id ?? 0 })}
    draggable="true"
    on:dragstart={(e) => onFeedDragStart(e, f)}
    on:dragover={(e) => onDragOver(e, { kind: "feed", id: f.subscription_id, cat: f.category_id ?? 0 })}
    on:dragleave={() => (dropTarget = null)}
    on:drop={(e) => onFeedDrop(e, f)}
    on:dragend={onDragEnd}>
    <button
      class="feed-item"
      class:active={isActiveFeed(f.id)}
      class:read={f.unread === 0}
      on:click={() => pickFeed(f.id)}
      data-testid="feed-{f.id}"
    >
      <span class="favicon" style="background:{favColor(f.id)}">{faviconLetter(f.title_override || f.title)}</span>
      <span class="ni-label">{f.title_override || f.title}</span>
      {#if (f.error_count ?? 0) >= 3}
        <span class="error-tag" aria-label="feed errored" title={`${f.error_count} consecutive errors: ${f.last_error || "unknown"}`}>!</span>
      {/if}
      {#if f.muted}<span class="muted-tag" aria-label="muted">🔕</span>{/if}
      {#if f.unread > 0 && !f.muted}<span class="badge">{f.unread}</span>{/if}
    </button>
    <button
      class="feed-actions-trigger"
      data-feed-actions-trigger
      data-testid="feed-actions-{f.id}"
      on:click={(e) => {
        e.stopPropagation();
        menuFor = menuFor === f.id ? null : f.id;
      }}
      aria-label="Feed actions"
      title="More"
    >
      <svg viewBox="0 0 24 24" width="14" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="5" r="1" /><circle cx="12" cy="12" r="1" /><circle cx="12" cy="19" r="1" />
      </svg>
    </button>
    {#if menuFor === f.id}
      <div class="feed-menu" data-feed-menu-for={f.id}>
        <button on:click={() => markFeedRead(f)} data-testid="feed-mark-read-{f.id}">
          Mark feed read
        </button>
        <button on:click={() => toggleMute(f)} data-testid="feed-mute-{f.id}">
          {f.muted ? "Unmute" : "Mute"}
        </button>
        <button on:click={() => resummarize(f)} data-testid="feed-resummarize-{f.id}">
          Resummarize
        </button>
        <button class="danger" on:click={() => deleteFeed(f)} data-testid="feed-delete-{f.id}">
          Delete
        </button>
      </div>
    {/if}
  </div>
{/snippet}

<aside class="rail">
  <!-- Scrolling content wrapper. Lets the .summarizing footer below stay
       pinned to the bottom of the rail regardless of scroll position. -->
  <div class="rail-scroll">
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
      {#if $smartCounts.fresh > 0}<span class="badge" data-testid="badge-fresh">{$smartCounts.fresh}</span>{/if}
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
      {#if $smartCounts.starred > 0}<span class="badge" data-testid="badge-starred">{$smartCounts.starred}</span>{/if}
    </button>
    <button class="nav-item" class:active={isActiveSmart("later")} on:click={() => pickSmart("later")}>
      <span class="ni-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" /></svg>
      </span>
      <span class="ni-label">Read Later</span>
      {#if $smartCounts.later > 0}<span class="badge" data-testid="badge-later">{$smartCounts.later}</span>{/if}
    </button>
    <button class="nav-item" class:active={isActiveSmart("shared")} on:click={() => pickSmart("shared")}>
      <span class="ni-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="18" cy="5" r="3" /><circle cx="6" cy="12" r="3" /><circle cx="18" cy="19" r="3" /><path d="M8.6 13.5l6.8 4M15.4 6.5l-6.8 4" /></svg>
      </span>
      <span class="ni-label">Shared with me</span>
      {#if $smartCounts.shared > 0}<span class="badge" data-testid="badge-shared">{$smartCounts.shared}</span>{/if}
    </button>
  </div>

  <!-- Folders + feeds -->
  <div class="rail-section">
    <div class="rail-head"><h3>Folders</h3></div>

    {#each $categories as cat (cat.id)}
      <div
        class="folder"
        class:collapsed={collapsedCategories[cat.id]}
        class:drop-target={sameDrag(dropTarget, { kind: "folder", id: cat.id })}
      >
        <div
          class="folder-head"
          draggable="true"
          on:dragstart={(e) => onFolderDragStart(e, cat.id)}
          on:dragover={(e) => onDragOver(e, { kind: "folder", id: cat.id })}
          on:dragleave={() => (dropTarget = null)}
          on:drop={(e) => onFolderDrop(e, cat.id)}
          on:dragend={onDragEnd}
        >
          <button class="chev-btn" on:click={() => toggleCategory(cat.id)} aria-label="Toggle folder">
            <svg class="chev" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M6 9l6 6 6-6" /></svg>
          </button>
          <span class="dot" style="background:{cat.color || dotColor(cat.id)}"></span>
          {#if renamingCategoryID === cat.id}
            <input
              class="folder-rename"
              bind:value={renameValue}
              on:keydown={(e) => {
                if (e.key === "Enter") commitRename(cat.id);
                if (e.key === "Escape") cancelRename();
              }}
              on:blur={() => commitRename(cat.id)}
              autofocus
              data-testid="folder-rename-{cat.id}"
            />
          {:else}
            <button
              class="folder-name"
              class:active={isActiveCategory(cat.id)}
              on:click={() => pickCategory(cat.id)}
              on:dblclick={() => startRenameCategory(cat.id, cat.name)}
              data-testid="folder-name-{cat.id}"
            >
              {cat.name}
            </button>
          {/if}
          {#if unreadInCategory(cat.id) > 0 && renamingCategoryID !== cat.id}
            <span class="badge">{unreadInCategory(cat.id)}</span>
          {/if}
          <button
            class="folder-actions-trigger"
            data-cat-actions-trigger
            data-testid="folder-actions-{cat.id}"
            on:click={(e) => {
              e.stopPropagation();
              categoryMenuFor = categoryMenuFor === cat.id ? null : cat.id;
              colorPickerFor = null;
            }}
            aria-label="Folder actions"
          >
            <svg viewBox="0 0 24 24" width="13" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="5" r="1" /><circle cx="12" cy="12" r="1" /><circle cx="12" cy="19" r="1" /></svg>
          </button>
          {#if categoryMenuFor === cat.id}
            <div class="folder-menu" data-cat-menu-for={cat.id}>
              <button on:click={() => startRenameCategory(cat.id, cat.name)} data-testid="folder-rename-action-{cat.id}">Rename</button>
              <button on:click={() => { colorPickerFor = cat.id; categoryMenuFor = null; }} data-testid="folder-color-action-{cat.id}">Color…</button>
              <button class="danger" on:click={() => deleteCategory(cat.id, cat.name)} data-testid="folder-delete-action-{cat.id}">Delete</button>
            </div>
          {/if}
          {#if colorPickerFor === cat.id}
            <div class="folder-color-picker" data-cat-color-for={cat.id}>
              {#each CAT_PALETTE as c (c)}
                <button
                  class="swatch"
                  style="background:{c}"
                  on:click={() => pickCategoryColor(cat.id, c)}
                  aria-label={`Pick color ${c}`}
                  data-testid="folder-swatch-{cat.id}-{c}"
                ></button>
              {/each}
            </div>
          {/if}
        </div>
        <div class="feed-list">
          {#each grouped.byCat.get(cat.id) ?? [] as f (f.id)}
            {@render feedRow(f)}
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
            {@render feedRow(f)}
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

  <!-- Saved searches -->
  <div class="rail-section">
    <div class="rail-head">
      <h3>Saved searches</h3>
      <button
        class="head-add"
        on:click={() => (newSearchFormOpen = !newSearchFormOpen)}
        aria-label="New saved search"
        title="Save a search"
        data-testid="open-add-search"
      >+</button>
    </div>

    {#if newSearchFormOpen}
      <form class="add-form add-form-board" on:submit={submitNewSavedSearch}>
        <input
          type="text"
          bind:value={newSearchName}
          placeholder="Name (e.g. 'rust news')"
          disabled={creatingSearch}
          data-testid="add-search-name"
        />
        <input
          type="text"
          bind:value={newSearchQuery}
          placeholder="Query — FTS5 syntax"
          disabled={creatingSearch}
          data-testid="add-search-query"
        />
        <div class="add-form-actions">
          <button type="button" class="ghost" on:click={() => { newSearchFormOpen = false; newSearchName = ""; newSearchQuery = ""; }}>
            Cancel
          </button>
          <button type="submit" disabled={creatingSearch || !newSearchName.trim() || !newSearchQuery.trim()} data-testid="add-search-submit">
            {creatingSearch ? "…" : "Save"}
          </button>
        </div>
      </form>
    {/if}

    {#each $savedSearches as ss (ss.id)}
      <div class="feed-row board-row">
        <button
          class="nav-item board-item"
          class:active={isActiveSavedSearch(ss.id)}
          on:click={() => pickSavedSearch(ss.id, ss.query)}
          data-testid="saved-search-{ss.id}"
        >
          <span class="ni-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7" /><path d="M21 21l-4-4" /></svg>
          </span>
          <span class="ni-label">{ss.name}</span>
        </button>
        <button
          class="board-delete"
          on:click={() => deleteSavedSearch(ss.id, ss.name)}
          aria-label="Delete saved search"
          title="Delete saved search"
          data-testid="saved-search-delete-{ss.id}"
        >
          ×
        </button>
      </div>
    {/each}
  </div>

  <!-- Boards -->
  <div class="rail-section">
    <div class="rail-head">
      <h3>Boards</h3>
      <button
        class="head-add"
        on:click={() => (newBoardFormOpen = !newBoardFormOpen)}
        aria-label="New board"
        title="New board"
        data-testid="open-add-board"
      >+</button>
    </div>

    {#if newBoardFormOpen}
      <form class="add-form add-form-board" on:submit={submitNewBoard}>
        <input
          type="text"
          bind:value={newBoardName}
          placeholder="Board name"
          disabled={creatingBoard}
          data-testid="add-board-input"
        />
        <div class="add-form-actions">
          <button type="button" class="ghost" on:click={() => { newBoardFormOpen = false; newBoardName = ""; }}>
            Cancel
          </button>
          <button type="submit" disabled={creatingBoard || !newBoardName.trim()} data-testid="add-board-submit">
            {creatingBoard ? "…" : "Add"}
          </button>
        </div>
      </form>
    {/if}

    {#each $boards as b (b.id)}
      <div class="feed-row board-row">
        <button
          class="nav-item board-item"
          class:active={isActiveBoard(b.id)}
          on:click={() => pickBoard(b.id)}
          data-testid="board-{b.id}"
        >
          <span class="ni-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /></svg>
          </span>
          <span class="ni-label">{b.name}</span>
        </button>
        <button
          class="board-delete"
          on:click={() => deleteBoard(b.id, b.name)}
          aria-label="Delete board"
          title="Delete board"
          data-testid="board-delete-{b.id}"
        >
          ×
        </button>
      </div>
    {/each}
  </div>
  </div><!-- /.rail-scroll -->

  <!-- Summarizer status footer: pinned to the bottom of the rail (outside
       .rail-scroll) so it stays visible regardless of scroll position.
       Shown only while the poller's summary worker has articles to chew
       through. Updates piggyback on refreshSidebar() (login, polling tick,
       navigation refresh) so the count stays roughly live. -->
  {#if $smartCounts.pending_summary > 0}
    <div class="summarizing" data-testid="sidebar-summarizing">
      <span class="summarizing-dot" aria-hidden="true"></span>
      <span class="summarizing-label">
        Summarizing {$smartCounts.pending_summary}
        {$smartCounts.pending_summary === 1 ? "article" : "articles"}…
      </span>
    </div>
  {/if}
</aside>

{#if confirmReq}
  <ConfirmDialog
    title={confirmReq.title}
    message={confirmReq.message}
    confirmLabel={confirmReq.confirmLabel ?? "Confirm"}
    destructive={confirmReq.destructive ?? false}
    busy={confirmBusy}
    onConfirm={runConfirm}
    onCancel={() => (confirmReq = null)}
  />
{/if}

<style>
  .rail {
    border-right: 1px solid var(--line);
    background: var(--paper-2);
    /* Flex column so the .summarizing footer (last child) stays pinned to
       the bottom while .rail-scroll consumes remaining height + scrolls. */
    display: flex;
    flex-direction: column;
    /* Take the full available height of the layout grid track so the footer
       has a viewport to anchor to. */
    height: 100%;
    min-height: 0;
  }
  .rail-scroll {
    flex: 1 1 auto;
    overflow-y: auto;
    padding: 14px 12px 40px;
    min-height: 0;
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
    color: var(--ink-soft);
    background: var(--line);
    padding: 1px 6px;
    border-radius: 20px;
    min-width: 20px;
    text-align: center;
  }
  .nav-item.active .badge { background: var(--ember); color: #fff; }

  .folder { margin-bottom: 2px; }
  .folder.drop-target > .folder-head {
    box-shadow: 0 -2px 0 var(--ember) inset;
  }
  .folder-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-radius: 9px;
    transition: background 0.12s;
    position: relative;
    cursor: grab;
  }
  .folder-head:active { cursor: grabbing; }
  .folder-head:hover { background: var(--line-soft); }
  .folder-head:hover .folder-actions-trigger { opacity: 1; }
  .folder-rename {
    flex: 1;
    padding: 3px 7px;
    border: 1px solid var(--ember);
    border-radius: 5px;
    font: inherit;
    font-size: 12.5px;
    background: var(--card);
    color: var(--ink);
    outline: none;
  }
  .folder-actions-trigger {
    width: 20px;
    height: 20px;
    border-radius: 5px;
    display: grid;
    place-items: center;
    color: var(--ink-faint);
    background: transparent;
    border: none;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.12s, background 0.12s;
    margin-left: auto;
  }
  .folder-actions-trigger:hover { background: var(--line); color: var(--ink); }
  .folder-actions-trigger:focus-visible { opacity: 1; outline: none; }
  .folder-menu {
    position: absolute;
    right: 4px;
    top: calc(100% + 2px);
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 8px;
    box-shadow: var(--shadow-pane);
    padding: 4px;
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 120px;
  }
  .folder-menu button {
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12px;
    color: var(--ink);
    text-align: left;
    background: transparent;
    border: none;
    cursor: pointer;
  }
  .folder-menu button:hover { background: var(--line-soft); }
  .folder-menu button.danger { color: #b91c1c; }
  .folder-menu button.danger:hover { background: #fef2f2; }
  .folder-color-picker {
    position: absolute;
    right: 4px;
    top: calc(100% + 2px);
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 8px;
    box-shadow: var(--shadow-pane);
    padding: 6px;
    z-index: 30;
    display: grid;
    grid-template-columns: repeat(4, 22px);
    gap: 4px;
  }
  .folder-color-picker .swatch {
    width: 22px;
    height: 22px;
    border-radius: 5px;
    border: 1px solid var(--line);
    cursor: pointer;
    padding: 0;
  }
  .folder-color-picker .swatch:hover { transform: scale(1.1); }
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
  .feed-row {
    position: relative;
    display: flex;
    align-items: center;
    border-radius: 8px;
    cursor: grab;
  }
  .feed-row:active { cursor: grabbing; }
  .feed-row.drop-target {
    box-shadow: 0 -2px 0 var(--ember) inset;
  }
  .feed-row.muted .ni-label { color: var(--ink-faint); font-style: italic; }
  .feed-row.errored .ni-label { color: var(--ink-faint); }
  .error-tag {
    width: 16px;
    height: 16px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-weight: 800;
    font-size: 11px;
    border-radius: 50%;
    background: #b3261e;
    color: #fff;
    cursor: help;
  }
  .feed-row:hover .feed-actions-trigger { opacity: 1; }
  .feed-item {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: 1;
    min-width: 0;
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
  .muted-tag {
    margin-left: auto;
    font-size: 12px;
    opacity: 0.7;
  }
  .feed-actions-trigger {
    position: absolute;
    right: 4px;
    top: 50%;
    transform: translateY(-50%);
    width: 22px;
    height: 22px;
    border-radius: 6px;
    display: grid;
    place-items: center;
    color: var(--ink-faint);
    opacity: 0;
    transition: opacity 0.12s, background 0.12s;
    background: var(--paper-2);
    border: none;
    cursor: pointer;
  }
  .feed-actions-trigger:hover { background: var(--line); color: var(--ink); }
  .feed-actions-trigger:focus-visible { opacity: 1; outline: none; }
  .feed-menu {
    position: absolute;
    right: 4px;
    top: calc(100% + 2px);
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 8px;
    box-shadow: var(--shadow-pane);
    padding: 4px;
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 120px;
  }
  .feed-menu button {
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12px;
    color: var(--ink);
    text-align: left;
    background: transparent;
    border: none;
    cursor: pointer;
  }
  .feed-menu button:hover { background: var(--line-soft); }
  .feed-menu button.danger { color: #b91c1c; }
  .feed-menu button.danger:hover { background: #fef2f2; }
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

  .head-add {
    background: transparent;
    border: none;
    color: var(--ink-faint);
    font-size: 18px;
    line-height: 1;
    width: 20px;
    height: 20px;
    border-radius: 6px;
    cursor: pointer;
  }
  .head-add:hover { background: var(--line-soft); color: var(--ember); }
  .add-form-board { padding: 0 10px 4px; }
  .board-row { padding-right: 28px; }
  .board-item { padding-right: 4px; }
  .board-delete {
    position: absolute;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    width: 18px;
    height: 18px;
    border-radius: 4px;
    background: transparent;
    border: none;
    color: var(--ink-faint);
    opacity: 0;
    cursor: pointer;
    line-height: 1;
    font-size: 14px;
  }
  .board-row:hover .board-delete { opacity: 1; }
  .board-delete:hover { background: var(--line); color: #b91c1c; }

  /* Summarizer status footer. Sits OUTSIDE .rail-scroll so it stays
     pinned to the bottom of the rail viewport regardless of scroll
     position. Full-width footer band with a top border to separate
     visually from the scrolling list. Hidden when nothing is pending
     (no DOM, not visibility:hidden). */
  .summarizing {
    flex: 0 0 auto;
    padding: 10px 14px;
    background: var(--ember-wash);
    border-top: 1px solid var(--line-soft);
    color: var(--ink-soft);
    font-size: 12.5px;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .summarizing-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--ember);
    animation: pulse 1.6s ease-in-out infinite;
    flex: 0 0 auto;
  }
  @keyframes pulse {
    0%, 100% { opacity: 0.4; transform: scale(0.85); }
    50%      { opacity: 1;   transform: scale(1.1); }
  }
  .summarizing-label {
    line-height: 1.3;
  }
</style>
