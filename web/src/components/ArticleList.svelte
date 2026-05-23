<script lang="ts">
  import { onDestroy } from "svelte";
  import {
    articles,
    selectedArticleId,
    setRead,
    toggleStar,
  } from "../lib/stores";

  let containerEl: HTMLDivElement | undefined = $state();
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

  // Single IO instance, recreated when the container element appears.
  let io: IntersectionObserver | undefined;
  // Track which items we've already seen as visible. We only mark "read" the
  // first time an item transitions from intersecting → not intersecting.
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
    // Re-observe whenever the items array identity changes.
    void $articles.items;
    // Defer to next microtask so the new DOM nodes exist.
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
</script>

<div class="article-list" bind:this={containerEl} data-testid="article-list">
  {#if $articles.loading && $articles.items.length === 0}
    <p class="empty">Loading…</p>
  {:else if $articles.err}
    <p class="empty error">Error: {$articles.err}</p>
  {:else if $articles.items.length === 0}
    <p class="empty">No articles in this view.</p>
  {/if}

  {#each $articles.items as a (a.id)}
    <article
      class="card"
      class:read={a.is_read}
      class:selected={$selectedArticleId === a.id}
      data-article-id={a.id}
      data-is-read={a.is_read ? "1" : "0"}
      data-testid="story-{a.id}"
    >
      <button class="hit" on:click={() => select(a.id)}>
        <h3>{a.title}</h3>
        {#if a.author}<p class="byline">{a.author}</p>{/if}
        {#if a.content_text}
          <p class="excerpt">{a.content_text.slice(0, 220)}</p>
        {/if}
      </button>
      <div class="actions">
        <button
          class="icon"
          class:active={a.is_starred}
          on:click={() => toggleStar(a.id, !a.is_starred)}
          aria-label="Star"
          data-testid="star-{a.id}"
        >
          ★
        </button>
      </div>
    </article>
  {/each}
</div>

<style>
  .article-list {
    flex: 1 1 360px;
    overflow-y: auto;
    background: var(--bg);
    padding: 1rem;
    border-right: 1px solid var(--border);
  }
  .card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.75rem 1rem;
    margin-bottom: 0.75rem;
    display: flex;
    gap: 0.75rem;
    align-items: flex-start;
  }
  .card.read { opacity: 0.55; }
  .card.selected { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent-bg); }
  .hit {
    flex: 1;
    text-align: left;
    background: transparent;
    border: 0;
    padding: 0;
    cursor: pointer;
    color: inherit;
    font: inherit;
  }
  h3 { margin: 0 0 0.25rem; font-family: "Fraunces", serif; font-size: 1rem; }
  .byline { color: var(--muted); margin: 0 0 0.5rem; font-size: 0.8rem; }
  .excerpt {
    margin: 0;
    color: var(--muted);
    font-size: 0.85rem;
    line-height: 1.4;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .actions { display: flex; gap: 0.25rem; }
  .icon {
    background: transparent;
    border: 0;
    cursor: pointer;
    font-size: 1.2rem;
    color: var(--muted);
  }
  .icon.active { color: var(--accent); }
  .empty {
    color: var(--muted);
    text-align: center;
    padding: 2rem;
  }
  .empty.error { color: #b91c1c; }
</style>
