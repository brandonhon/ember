<script lang="ts">
  import {
    activeView,
    articles,
    boards,
    categories,
    density,
    feeds,
    loadArticles,
    refreshSidebar,
    selectedArticleId,
    toggleStar,
    toggleLater,
    newArticleCount,
    freshWindowSeconds,
  } from "../lib/stores";
  import { api } from "../lib/api";
  import { get } from "svelte/store";
  import type { ArticleView, ClusterSibling, FeedWithCounts } from "../lib/types";

  let containerEl: HTMLElement | undefined = $state();
  let freshOnly = $state(false);
  let unreadOnly = $state(false);

  // Cluster expansion: clicking the "Also in N" pill opens a popover
  // listing the cross-feed siblings (other feeds the user is subscribed
  // to that also published this URL after canonicalization). Lazy-fetched
  // on first open per article id; cached for the lifetime of the list.
  let clusterOpenForId = $state<number | null>(null);
  let clusterSiblingsCache = $state<Record<number, ClusterSibling[] | "loading" | "error">>({});
  async function openCluster(e: Event, id: number) {
    e.stopPropagation();
    e.preventDefault();
    if (clusterOpenForId === id) {
      clusterOpenForId = null;
      return;
    }
    clusterOpenForId = id;
    if (clusterSiblingsCache[id] !== undefined) return;
    clusterSiblingsCache[id] = "loading";
    try {
      const res = await api.getArticleCluster(id);
      clusterSiblingsCache[id] = res.data?.siblings ?? [];
    } catch {
      clusterSiblingsCache[id] = "error";
    }
  }
  function onClusterKey(e: KeyboardEvent, id: number) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      void openCluster(e, id);
    } else if (e.key === "Escape") {
      clusterOpenForId = null;
    }
  }
  // Close popover on any outside click.
  function onListClick(e: MouseEvent) {
    if (clusterOpenForId === null) return;
    const t = e.target as HTMLElement;
    if (!t.closest("[data-cluster-popover]") && !t.closest("[data-cluster-trigger]")) {
      clusterOpenForId = null;
    }
  }

  // Reset scroll to the top whenever the active view changes (item 8 — user
  // clicks Today/Fresh/All Unread/Starred/feed/folder). Instant scroll matches
  // "be taken to the top." Touching $activeView gets Svelte 5 to react to the
  // store change. We re-read containerEl each time because the effect may run
  // before bind:this resolves on first paint.
  $effect(() => {
    void $activeView;
    if (containerEl) containerEl.scrollTop = 0;
  });

  function select(id: number) {
    selectedArticleId.set(id);
  }

  function clearActiveSearch() {
    const fresh = { kind: "smart" as const, view: "fresh" as const };
    activeView.set(fresh);
    void loadArticles(fresh);
  }

  // When the user scrolls to the top of the list, they've "seen" the new
  // articles — clear the favicon-dot counter.
  function onScroll() {
    if (!containerEl) return;
    if (containerEl.scrollTop <= 12 && get(newArticleCount) > 0) {
      newArticleCount.set(0);
    }
  }

  const headerTitle = $derived.by(() => {
    switch ($activeView.kind) {
      case "smart":
        return {
          fresh: "Fresh",
          today: "Today",
          unread: "All Unread",
          starred: "Starred",
          later: "Read Later",
          shared: "Shared with me",
        }[$activeView.view];
      case "feed": {
        const f = $feeds.find((x) => x.id === ($activeView as { id: number }).id);
        return f ? f.title_override || f.title : "Feed";
      }
      case "category": {
        const c = $categories.find((x) => x.id === ($activeView as { id: number }).id);
        return c ? c.name : "Folder";
      }
      case "board": {
        const b = $boards.find((x) => x.id === ($activeView as { id: number }).id);
        return b ? b.name : "Board";
      }
      case "search":
        return `Search: ${$activeView.query}`;
    }
  });
  const headerSub = $derived.by(() => {
    const n = $articles.items.length;
    if ($articles.loading) return "Loading…";
    if (n === 0) return "No articles";
    return `${n} article${n === 1 ? "" : "s"}`;
  });

  const feedById = $derived.by(() => {
    const m = new Map<number, FeedWithCounts>();
    for (const f of $feeds) m.set(f.id, f);
    return m;
  });
  const FAV_COLORS = ["#ff6154", "#0a0a0a", "#e63946", "#1d4ed8", "#623ce6", "#ee0000", "#326ce5", "#111", "#cc0000", "#bb1919"];
  function favColor(feedID: number): string {
    return FAV_COLORS[feedID % FAV_COLORS.length];
  }
  function srcName(a: ArticleView): string {
    const f = feedById.get(a.feed_id);
    return f ? f.title_override || f.title : "—";
  }
  function srcInitial(a: ArticleView): string {
    return (srcName(a)[0] ?? "?").toUpperCase();
  }
  function timeAgo(unix: number | undefined): string {
    if (!unix) return "";
    const diff = Date.now() / 1000 - unix;
    if (diff < 60) return "just now";
    if (diff < 3600) return `${Math.round(diff / 60)} min ago`;
    if (diff < 86400) return `${Math.round(diff / 3600)} hr ago`;
    return `${Math.round(diff / 86400)} d ago`;
  }
  // Window comes from the server (EMBER_FRESH_WINDOW, surfaced via
  // /api/me) so the client filter matches the server's CountSmartViews
  // query + Fresh-list default cutoff. Defaults to 6h until /api/me
  // resolves on first paint.
  function isFresh(unix: number | undefined): boolean {
    if (!unix) return false;
    return Date.now() / 1000 - unix < $freshWindowSeconds;
  }
  // Reading-time estimate at 200 wpm. Falls back to stripping HTML tags out
  // of content_html when content_text is empty. Returns 0 (renders nothing)
  // when we have no body at all.
  function readingMinutes(a: ArticleView): number {
    const src = a.content_text || (a.content_html ? a.content_html.replace(/<[^>]+>/g, " ") : "");
    if (!src) return 0;
    const words = src.trim().split(/\s+/).length;
    return Math.max(1, Math.round(words / 200));
  }

  const filtered = $derived.by(() => {
    let out = $articles.items;
    if (freshOnly) out = out.filter((a) => isFresh(a.published_at));
    if (unreadOnly) out = out.filter((a) => !a.is_read);
    // Note: Fresh view used to re-sort read items to the bottom (PR #54),
    // but the user found the position-shifting jarring. Now read-fresh
    // items just visually fade via .story.read (opacity 0.62) and lose the
    // Fresh pill — they stay in their published_at position so the list
    // doesn't reflow under the cursor when an article gets marked read.
    return out;
  });

  async function onMarkAllRead() {
    let body: { feed_id?: number; category_id?: number; board_id?: number; view?: string } = {};
    if ($activeView.kind === "feed") body = { feed_id: $activeView.id };
    if ($activeView.kind === "category") body = { category_id: $activeView.id };
    if ($activeView.kind === "board") body = { board_id: $activeView.id };
    if ($activeView.kind === "smart") body = { view: $activeView.view };
    await api.markAllRead(body);
    await Promise.all([loadArticles(get(activeView)), refreshSidebar()]);
  }
</script>

<section class="list-col" bind:this={containerEl} on:scroll={onScroll} on:click={onListClick} role="presentation" data-testid="article-list">
  <div class="list-header">
    <div class="list-title-row">
      <div>
        <div class="list-title">
          {headerTitle}
          {#if $activeView.kind === "search"}
            <button
              class="clear-search-inline"
              on:click={clearActiveSearch}
              aria-label="Clear search"
              title="Back to Fresh"
              data-testid="list-clear-search"
            >×</button>
          {/if}
        </div>
        <div class="list-sub"><span class="poll-dot" aria-hidden="true"></span>{headerSub}</div>
      </div>
    </div>
    <div class="list-tools">
      <button
        class="pill fresh-toggle"
        class:on={freshOnly}
        on:click={() => (freshOnly = !freshOnly)}
        data-testid="pill-fresh"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9z" /></svg>
        Fresh only
      </button>
      <button
        class="pill"
        class:on={unreadOnly}
        on:click={() => (unreadOnly = !unreadOnly)}
        data-testid="pill-unread"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9" /></svg>
        Unread
      </button>
      <button class="pill" on:click={onMarkAllRead} data-testid="mark-all-read">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 6L9 17l-5-5" /></svg>
        Mark all read
      </button>
      <span class="spacer"></span>
      <div class="seg">
        <button class:on={$density === "card"} on:click={() => density.set("card")}>Cards</button>
        <button class:on={$density === "compact"} on:click={() => density.set("compact")}>Compact</button>
      </div>
    </div>
  </div>

  <div class="articles" class:compact={$density === "compact"}>
    {#if $articles.loading && filtered.length === 0}
      <p class="empty">Loading…</p>
    {:else if $articles.err}
      <p class="empty error">Error: {$articles.err}</p>
    {:else if filtered.length === 0 && $feeds.length === 0}
      <!-- Quiet empty state. The full welcome flow (starter-pack CTA + docs
           link) is rendered as a modal in App.svelte so it doesn't sit in
           the article column and block clicks on the topbar/sidebar. -->
      <p class="empty" data-testid="onboarding-empty">No feeds yet.</p>
    {:else if filtered.length === 0}
      <p class="empty">No articles in this view.</p>
    {/if}

    {#each filtered as a (a.id)}
      <article
        class="story"
        class:read={a.is_read}
        class:active={$selectedArticleId === a.id}
        data-article-id={a.id}
        data-is-read={a.is_read ? "1" : "0"}
        data-testid="story-{a.id}"
      >
        <span class="unread-dot" aria-hidden="true"></span>
        <!-- Primary action: clicking the title/thumb/excerpt area opens the
             story. A real <button> here keeps keyboard activation working
             while avoiding the nested-interactive a11y violation. Foot
             buttons are siblings below. -->
        <button
          class="story-link"
          on:click={() => select(a.id)}
          aria-label={`Open article: ${a.title}`}
        >
          <span class="story-top">
            <span class="src">
              <span class="favicon" style="background:{favColor(a.feed_id)}" aria-hidden="true">{srcInitial(a)}</span>
              {srcName(a)}
            </span>
            <span class="src-meta">· {timeAgo(a.published_at)}</span>
            {#if readingMinutes(a) > 0}
              <span class="src-meta">· {readingMinutes(a)} min read</span>
            {/if}
            {#if a.tags}
              <span class="tag-badge">{a.tags.split(",")[0].trim()}</span>
            {/if}
            {#if isFresh(a.published_at) && !a.is_read}<span class="fresh-tag">Fresh</span>{/if}
            {#if a.dup_count > 0}
              <!-- Visual label inside the story-link button. The interactive
                   trigger sits in story-foot below as a sibling — nested
                   buttons inside a button would be invalid HTML / a11y. -->
              <span class="dup-tag" title="Also published in other feeds you subscribe to" data-testid="dup-tag-{a.id}">
                Also in {a.dup_count + 1}
              </span>
            {/if}
          </span>
          <span class="story-title">{a.title}</span>
          {#if a.image_url}
            <img class="story-thumb" src={a.image_url} alt="" loading="lazy" on:error={(e) => ((e.currentTarget as HTMLImageElement).style.display = "none")} />
          {/if}
          {#if a.content_text}
            <span class="story-excerpt">{a.content_text}</span>
          {/if}
        </button>
        <div class="story-foot">
          <button
            class:starred={a.is_starred}
            on:click={() => toggleStar(a.id, !a.is_starred)}
            data-testid="star-{a.id}"
            aria-label={a.is_starred ? "Unstar article" : "Star article"}
          >
            {#if a.is_starred}
              <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 2l3 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.9 21l1.2-6.8-5-4.9 6.9-1z" /></svg>
              Starred
            {:else}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M12 2l3 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.9 21l1.2-6.8-5-4.9 6.9-1z" /></svg>
              Star
            {/if}
          </button>
          <button
            on:click={() => toggleLater(a.id, !a.is_later)}
            class:starred={a.is_later}
            aria-label={a.is_later ? "Remove from read later" : "Save for read later"}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" /></svg>
            {a.is_later ? "Saved" : "Later"}
          </button>
          {#if a.dup_count > 0}
            <span class="dup-wrap">
              <button
                class="dup-trigger"
                data-cluster-trigger
                on:click={(e) => openCluster(e, a.id)}
                on:keydown={(e) => onClusterKey(e, a.id)}
                aria-haspopup="true"
                aria-expanded={clusterOpenForId === a.id}
                aria-label={`View ${a.dup_count} other feed(s) that published this`}
                data-testid="dup-trigger-{a.id}"
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M8 7h12M8 12h12M8 17h12M3 7h.01M3 12h.01M3 17h.01"/></svg>
                Sources
              </button>
              {#if clusterOpenForId === a.id}
                <div class="cluster-popover" data-cluster-popover data-testid="cluster-popover-{a.id}" role="dialog" aria-label="Other feeds with this story">
                  {#if clusterSiblingsCache[a.id] === "loading"}
                    <div class="cluster-empty">Loading…</div>
                  {:else if clusterSiblingsCache[a.id] === "error"}
                    <div class="cluster-empty">Couldn't load.</div>
                  {:else if Array.isArray(clusterSiblingsCache[a.id]) && (clusterSiblingsCache[a.id] as ClusterSibling[]).length === 0}
                    <div class="cluster-empty">No other feeds carry this story.</div>
                  {:else}
                    {#each (clusterSiblingsCache[a.id] as ClusterSibling[]) as s (s.article_id)}
                      <a class="cluster-row" href={s.url || "#"} target="_blank" rel="noopener noreferrer">
                        <span class="cluster-feed">{s.feed_title || "Untitled feed"}</span>
                        <span class="cluster-state">
                          {#if s.is_starred}<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 2l3 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.9 21l1.2-6.8-5-4.9 6.9-1z"/></svg>{/if}
                          {s.is_read ? "read" : "unread"}
                        </span>
                      </a>
                    {/each}
                  {/if}
                </div>
              {/if}
            </span>
          {/if}
        </div>
      </article>
    {/each}
  </div>
</section>

<style>
  .list-col {
    border-right: 1px solid var(--line);
    background: var(--paper);
    overflow-y: auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  @media (max-width: 900px) {
    .list-col { border-right: 0; }
    .list-header { padding: 12px 14px 10px; }
    /* Pills + Cards/Compact seg grow to fill the row and the wrapped rows
       center, matching the reader-actions treatment on mobile. flex:1 1 auto
       lets siblings split row width evenly; min-width:0 lets them shrink
       past intrinsic label width instead of overflowing. */
    .list-tools {
      justify-content: center;
      gap: 6px;
    }
    .list-tools .pill,
    .list-tools .seg {
      flex: 1 1 auto;
      min-width: 0;
    }
    .list-tools .pill {
      justify-content: center;
      padding: 6px clamp(8px, 2vw, 14px);
      font-size: 12px;
    }
    /* The Cards|Compact segment is its own flex container — make both
       buttons inside split its width too so the two halves match width. */
    .list-tools .seg { display: flex; }
    .list-tools .seg button { flex: 1 1 0; padding: 6px clamp(8px, 2vw, 14px); }
    /* Spacer pushed .seg to the right on desktop; redundant with center. */
    .list-tools > .spacer { display: none; }
  }
  .list-header {
    position: sticky;
    top: 0;
    z-index: 5;
    background: var(--paper);
    border-bottom: 1px solid var(--line);
    padding: 14px 18px 11px;
  }
  .list-title-row {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 12px;
  }
  .list-title {
    font-family: var(--font-display);
    font-size: 21px;
    font-weight: 500;
    letter-spacing: -0.01em;
  }
  .clear-search-inline {
    background: var(--line-soft);
    border: 1px solid var(--line);
    color: var(--ink-soft);
    border-radius: 50%;
    width: 22px;
    height: 22px;
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
    margin-left: 8px;
    vertical-align: 4px;
  }
  .clear-search-inline:hover { color: var(--ember); border-color: var(--ember); }
  .list-sub {
    font-size: 11.5px;
    color: var(--ink-faint);
    margin-top: 3px;
  }
  .poll-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--green);
    display: inline-block;
    margin-right: 6px;
    animation: pulse 2.4s infinite;
    vertical-align: middle;
  }
  @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }
  .list-tools {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 11px;
    flex-wrap: wrap;
  }
  .pill {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    border-radius: 20px;
    border: 1px solid var(--line);
    font-size: 11.5px;
    font-weight: 600;
    color: var(--ink-soft);
    background: var(--card);
    transition: all 0.12s;
  }
  .pill:hover { border-color: var(--ink-faint); color: var(--ink); }
  .pill.on { background: var(--ink); color: var(--paper); border-color: var(--ink); }
  .pill svg { width: 13px; height: 13px; }
  .pill.fresh-toggle.on {
    background: var(--ember);
    border-color: var(--ember);
    color: #fff;
  }
  .seg {
    display: inline-flex;
    border: 1px solid var(--line);
    border-radius: 20px;
    overflow: hidden;
    background: var(--card);
  }
  .seg button {
    padding: 5px 10px;
    font-size: 11.5px;
    font-weight: 600;
    color: var(--ink-faint);
    background: transparent;
    border: none;
  }
  .seg button.on { background: var(--ink); color: var(--paper); }
  .spacer { flex: 1; }

  .articles { padding: 8px; flex: 1; }
  .story {
    display: block;
    width: 100%;
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 14px;
    padding: 14px;
    margin-bottom: 8px;
    box-shadow: var(--shadow-card);
    transition: border-color 0.14s;
    position: relative;
  }
  .story:hover { border-color: var(--ink-faint); }
  .story.active {
    border-color: var(--ember);
    box-shadow: 0 0 0 1px var(--ember), var(--shadow-card);
  }
  .story.read .story-title { color: var(--ink-faint); font-weight: 500; }
  /* Read-but-still-in-Fresh-window cards (PR-B item 7): visibly desaturated
     so the user can tell at a glance what's already been consumed. The card
     still opens normally; greying is informational, not interactive. */
  .story.read { opacity: 0.62; }
  .story.read:hover { opacity: 0.85; }
  .story-link {
    display: block;
    width: 100%;
    background: transparent;
    border: 0;
    padding: 0;
    text-align: left;
    cursor: pointer;
    color: inherit;
    font: inherit;
  }
  .story-link:focus-visible {
    outline: 2px solid var(--ember);
    outline-offset: 4px;
    border-radius: 6px;
  }
  .story .unread-dot {
    position: absolute;
    left: 6px;
    top: 20px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ember);
  }
  .story.read .unread-dot { display: none; }
  .story-top {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
    flex-wrap: wrap;
  }
  .src {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 11.5px;
    font-weight: 700;
    color: var(--ink-soft);
  }
  .src .favicon {
    width: 16px;
    height: 16px;
    border-radius: 5px;
    display: grid;
    place-items: center;
    font-size: 9px;
    font-weight: 800;
    color: #fff;
  }
  .src-meta {
    font-size: 11px;
    color: var(--ink-faint);
    font-weight: 500;
  }
  .fresh-tag {
    font-size: 9.5px;
    font-weight: 800;
    letter-spacing: 0.06em;
    color: var(--ember);
    background: var(--ember-wash);
    padding: 2px 7px;
    border-radius: 5px;
    text-transform: uppercase;
  }
  .dup-tag {
    font-size: 9.5px;
    font-weight: 700;
    letter-spacing: 0.04em;
    color: var(--ink-soft);
    background: var(--line-soft);
    padding: 2px 7px;
    border-radius: 5px;
    text-transform: uppercase;
    border: 1px solid var(--line);
  }
  .tag-badge {
    font-size: 9.5px;
    font-weight: 800;
    letter-spacing: 0.04em;
    color: var(--ink-soft);
    background: var(--line-soft);
    padding: 2px 7px;
    border-radius: 5px;
    text-transform: uppercase;
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .story-title {
    font-family: var(--font-display);
    font-size: 15.5px;
    line-height: 1.24;
    font-weight: 600;
    letter-spacing: -0.005em;
    color: var(--ink);
    display: block;
  }
  .story-excerpt {
    font-family: var(--font-read);
    font-size: 13px;
    line-height: 1.5;
    color: var(--ink-soft);
    margin-top: 6px;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .story-thumb {
    display: block;
    width: 100%;
    max-width: 320px;
    max-height: 180px;
    aspect-ratio: 16 / 9;
    border-radius: 10px;
    margin: 10px 0 4px;
    object-fit: cover;
    background: var(--line-soft);
  }
  .articles.compact .story-thumb { display: none; }
  .story-foot {
    display: flex;
    align-items: center;
    gap: 14px;
    margin-top: 11px;
    color: var(--ink-faint);
  }
  .story-foot button {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 11px;
    font-weight: 600;
    color: var(--ink-faint);
    border-radius: 6px;
    padding: 2px 4px;
    background: transparent;
    border: none;
  }
  .story-foot button:hover { color: var(--ember); }
  .story-foot button.starred { color: var(--gold); }
  .story-foot svg { width: 14px; height: 14px; }

  /* Cross-feed cluster ("Sources") popover. Anchored to the trigger inside
     .story-foot and overlays the story card. Width capped so it never
     overflows a narrow viewport. */
  .dup-wrap { position: relative; display: inline-flex; }
  .dup-trigger { /* inherits .story-foot button look */ }
  .cluster-popover {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 20;
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 10px;
    box-shadow: var(--shadow-pane);
    padding: 6px;
    min-width: 220px;
    max-width: min(360px, 88vw);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .cluster-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 7px 10px;
    border-radius: 7px;
    text-decoration: none;
    color: var(--ink);
    font-size: 12.5px;
  }
  .cluster-row:hover { background: var(--line-soft); }
  .cluster-feed {
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cluster-state {
    color: var(--ink-faint);
    font-size: 11px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
  }
  .cluster-state svg { width: 11px; height: 11px; color: var(--gold); }
  .cluster-empty {
    color: var(--ink-faint);
    font-size: 12px;
    padding: 10px 12px;
    text-align: center;
  }

  .articles.compact .story {
    padding: 10px 13px 10px 15px;
    border-radius: 10px;
    margin-bottom: 5px;
  }
  .articles.compact .story-excerpt,
  .articles.compact .story-foot { display: none; }
  .articles.compact .story-top { margin-bottom: 4px; }
  .articles.compact .story-title { font-size: 13.5px; }

  .empty {
    color: var(--ink-faint);
    text-align: center;
    padding: 2.5rem 1rem;
    font-family: var(--font-display);
  }
  .empty.error { color: #b91c1c; }

  .onboarding {
    margin: 40px 20px 0;
    padding: 28px 24px;
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 14px;
    text-align: center;
  }
  .onboarding h2 {
    font-family: var(--font-display);
    font-weight: 500;
    font-size: 22px;
    margin: 0 0 8px;
    color: var(--ink);
  }
  .onboarding p {
    color: var(--ink-soft);
    font-size: 13.5px;
    margin: 0 0 18px;
    line-height: 1.5;
  }
  .onboarding-actions { display: flex; gap: 8px; justify-content: center; margin-bottom: 14px; }
  .onboarding-actions a {
    padding: 8px 16px;
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 13px;
    font-weight: 600;
    text-decoration: none;
  }
  .onboarding-actions a.primary {
    background: var(--ember);
    color: #fff;
  }
  .onboarding-actions a.primary:hover { background: var(--ember-soft); }
  .onboarding-actions a.ghost {
    background: transparent;
    color: var(--ink);
    border: 1px solid var(--line);
  }
  .onboarding-actions a.ghost:hover { background: var(--line-soft); }
  .onboarding-hint {
    font-size: 12px;
    color: var(--ink-faint);
    margin: 0;
  }
</style>
