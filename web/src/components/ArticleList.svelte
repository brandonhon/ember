<script lang="ts">
  import { onDestroy } from "svelte";
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
    setRead,
    toggleStar,
    toggleLater,
  } from "../lib/stores";
  import { api } from "../lib/api";
  import { get } from "svelte/store";
  import type { ArticleView, FeedWithCounts } from "../lib/types";

  let containerEl: HTMLDivElement | undefined = $state();
  let freshOnly = $state(false);
  let unreadOnly = $state(false);
  const pendingRead = new Set<number>();
  let flushTimer: ReturnType<typeof setTimeout> | undefined;

  function flush() {
    if (pendingRead.size === 0) return;
    const ids = Array.from(pendingRead);
    pendingRead.clear();
    void setRead(ids, true);
  }
  function scheduleFlush() {
    if (flushTimer) clearTimeout(flushTimer);
    flushTimer = setTimeout(flush, 350);
  }

  let io: IntersectionObserver | undefined;
  const sawVisible = new Set<string>();

  $effect(() => {
    if (!containerEl) return;
    io?.disconnect();
    sawVisible.clear();
    io = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const target = entry.target as HTMLElement;
          const idAttr = target.dataset.articleId;
          const isRead = target.dataset.isRead === "1";
          if (!idAttr) continue;
          if (entry.isIntersecting) {
            sawVisible.add(idAttr);
          } else if (sawVisible.has(idAttr) && !isRead) {
            pendingRead.add(Number(idAttr));
            scheduleFlush();
          }
        }
      },
      { root: containerEl, threshold: 0 },
    );
    void $articles.items;
    queueMicrotask(() => {
      if (!containerEl || !io) return;
      const els = containerEl.querySelectorAll<HTMLElement>("[data-article-id]");
      els.forEach((el) => io!.observe(el));
    });
  });

  onDestroy(() => {
    flush();
    io?.disconnect();
  });

  function select(id: number) {
    selectedArticleId.set(id);
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
  function isFresh(unix: number | undefined): boolean {
    if (!unix) return false;
    return Date.now() / 1000 - unix < 6 * 3600;
  }

  const filtered = $derived.by(() => {
    let out = $articles.items;
    if (freshOnly) out = out.filter((a) => isFresh(a.published_at));
    if (unreadOnly) out = out.filter((a) => !a.is_read);
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

<section class="list-col" bind:this={containerEl} data-testid="article-list">
  <div class="list-header">
    <div class="list-title-row">
      <div>
        <div class="list-title">{headerTitle}</div>
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
    {:else if filtered.length === 0}
      <p class="empty">No articles in this view.</p>
    {/if}

    {#each filtered as a (a.id)}
      <div
        class="story"
        class:read={a.is_read}
        class:active={$selectedArticleId === a.id}
        data-article-id={a.id}
        data-is-read={a.is_read ? "1" : "0"}
        data-testid="story-{a.id}"
        role="button"
        tabindex="0"
        on:click={() => select(a.id)}
        on:keydown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            select(a.id);
          }
        }}
      >
        <span class="unread-dot" aria-hidden="true"></span>
        <div class="story-top">
          <span class="src">
            <span class="favicon" style="background:{favColor(a.feed_id)}">{srcInitial(a)}</span>
            {srcName(a)}
          </span>
          <span class="src-meta">· {timeAgo(a.published_at)}</span>
          {#if a.tags}
            <span class="tag-badge">{a.tags.split(",")[0].trim()}</span>
          {/if}
          {#if isFresh(a.published_at)}<span class="fresh-tag">Fresh</span>{/if}
        </div>
        {#if a.image_url}
          <img class="story-thumb" src={a.image_url} alt="" loading="lazy" on:error={(e) => ((e.currentTarget as HTMLImageElement).style.display = "none")} />
        {/if}
        <div class="story-title">{a.title}</div>
        {#if a.content_text}
          <div class="story-excerpt">{a.content_text}</div>
        {/if}
        <div class="story-foot">
          <button
            class:starred={a.is_starred}
            on:click|stopPropagation={() => toggleStar(a.id, !a.is_starred)}
            data-testid="star-{a.id}"
          >
            {#if a.is_starred}
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2l3 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.9 21l1.2-6.8-5-4.9 6.9-1z" /></svg>
              Starred
            {:else}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2l3 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.9 21l1.2-6.8-5-4.9 6.9-1z" /></svg>
              Star
            {/if}
          </button>
          <button on:click|stopPropagation={() => toggleLater(a.id, !a.is_later)} class:starred={a.is_later}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" /></svg>
            {a.is_later ? "Saved" : "Later"}
          </button>
        </div>
      </div>
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
    text-align: left;
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 14px;
    padding: 14px;
    margin-bottom: 8px;
    box-shadow: var(--shadow-card);
    transition: border-color 0.14s;
    position: relative;
    cursor: pointer;
  }
  .story:hover { border-color: var(--ink-faint); }
  .story.active {
    border-color: var(--ember);
    box-shadow: 0 0 0 1px var(--ember), var(--shadow-card);
  }
  .story.read .story-title { color: var(--ink-faint); font-weight: 500; }
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
    float: right;
    width: 76px;
    height: 76px;
    border-radius: 10px;
    margin: 0 0 6px 14px;
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
</style>
