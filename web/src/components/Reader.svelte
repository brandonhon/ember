<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { articles, boards, feeds, selectedArticleId, toggleStar, toggleLater } from "../lib/stores";
  import type { FeedWithCounts } from "../lib/types";
  import { api, ApiError } from "../lib/api";
  import ShareModal from "./ShareModal.svelte";

  const selected = $derived(
    $selectedArticleId === null
      ? null
      : ($articles.items.find((a) => a.id === $selectedArticleId) ?? null),
  );

  const feed = $derived.by(() => {
    if (!selected) return null as FeedWithCounts | null;
    return $feeds.find((f) => f.id === selected.feed_id) ?? null;
  });

  let showShare = $state(false);
  let showBoardPicker = $state(false);
  let boardMsg = $state("");

  function onDocClick(e: MouseEvent) {
    if (!showBoardPicker) return;
    const t = e.target as HTMLElement;
    if (!t.closest("[data-board-picker]") && !t.closest("[data-board-trigger]")) {
      showBoardPicker = false;
    }
  }
  onMount(() => document.addEventListener("click", onDocClick));
  onDestroy(() => document.removeEventListener("click", onDocClick));

  async function addToBoard(boardID: number, boardName: string) {
    if (!selected) return;
    showBoardPicker = false;
    try {
      await api.addToBoard(boardID, selected.id);
      boardMsg = `Added to "${boardName}"`;
      setTimeout(() => (boardMsg = ""), 2400);
    } catch (err) {
      boardMsg = err instanceof ApiError ? err.message : String(err);
      setTimeout(() => (boardMsg = ""), 4000);
    }
  }

  const FAV_COLORS = ["#ff6154", "#0a0a0a", "#e63946", "#1d4ed8", "#623ce6", "#ee0000", "#326ce5", "#111", "#cc0000", "#bb1919"];
  function favColor(feedID: number): string {
    return FAV_COLORS[feedID % FAV_COLORS.length];
  }
  function timeAgo(unix: number | undefined): string {
    if (!unix) return "";
    const diff = Date.now() / 1000 - unix;
    if (diff < 60) return "just now";
    if (diff < 3600) return `${Math.round(diff / 60)} min ago`;
    if (diff < 86400) return `${Math.round(diff / 3600)} hr ago`;
    return `${Math.round(diff / 86400)} d ago`;
  }

  // Convert the stored bullet text into an array.
  const summaryLines = $derived.by(() => {
    if (!selected?.summary) return [] as string[];
    return selected.summary
      .split(/\n/)
      .map((s) => s.replace(/^[•\-\*]\s*/, "").trim())
      .filter((s) => s.length > 0);
  });
</script>

<section class="reader" id="reader">
  {#if !selected}
    <div class="reader-empty">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
        <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z" />
        <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z" />
      </svg>
      <div class="big">Pick a story</div>
      <div>Scroll the list to mark items read automatically.</div>
    </div>
  {:else}
    <div class="reader-inner">
      <div class="reader-actions">
        <button
          class="ra-btn"
          class:starred={selected.is_starred}
          on:click={() => toggleStar(selected.id, !selected.is_starred)}
          data-testid="reader-star"
        >
          {#if selected.is_starred}
            <svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2l3 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.9 21l1.2-6.8-5-4.9 6.9-1z" /></svg>
            Starred
          {:else}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2l3 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.9 21l1.2-6.8-5-4.9 6.9-1z" /></svg>
            Star
          {/if}
        </button>
        <button class="ra-btn" on:click={() => toggleLater(selected.id, !selected.is_later)} class:starred={selected.is_later}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" /></svg>
          {selected.is_later ? "Saved" : "Read later"}
        </button>
        <button class="ra-btn" on:click={() => (showShare = true)} data-testid="reader-share">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="18" cy="5" r="3" /><circle cx="6" cy="12" r="3" /><circle cx="18" cy="19" r="3" /><path d="M8.6 13.5l6.8 4M15.4 6.5l-6.8 4" /></svg>
          Share
        </button>
        <div class="board-wrap">
          <button
            class="ra-btn"
            on:click={(e) => { e.stopPropagation(); showBoardPicker = !showBoardPicker; }}
            data-board-trigger
            data-testid="reader-board"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /></svg>
            Board
          </button>
          {#if showBoardPicker}
            <div class="board-picker" data-board-picker>
              {#if $boards.length === 0}
                <div class="board-picker-empty">No boards yet. Create one in the sidebar.</div>
              {:else}
                {#each $boards as b (b.id)}
                  <button on:click={() => addToBoard(b.id, b.name)} data-testid="picker-board-{b.id}">
                    {b.name}
                  </button>
                {/each}
              {/if}
            </div>
          {/if}
          {#if boardMsg}
            <span class="board-msg">{boardMsg}</span>
          {/if}
        </div>
        <span style="flex:1"></span>
        {#if selected.url}
          <a class="ra-btn primary" href={selected.url} target="_blank" rel="noopener noreferrer">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" /><path d="M15 3h6v6M10 14L21 3" /></svg>
            Original
          </a>
        {/if}
      </div>

      <div class="article-kicker">
        <span class="favicon" style="background:{favColor(selected.feed_id)}">
          {(feed?.title || "?")[0]?.toUpperCase()}
        </span>
        <span class="src-name">{feed?.title_override || feed?.title || ""}</span>
        {#if selected.tags}
          <span class="source-badge">{selected.tags.split(",")[0].trim()}</span>
        {/if}
        <span class="src-time">· {timeAgo(selected.published_at)}</span>
      </div>

      <h1 class="article-h1">{selected.title}</h1>

      {#if selected.image_url}
        <figure class="article-hero">
          <img src={selected.image_url} alt="" loading="lazy" on:error={(e) => ((e.currentTarget as HTMLImageElement).style.display = "none")} />
        </figure>
      {/if}

      {#if summaryLines.length > 0}
        <aside class="ai-card" data-testid="summary-card">
          <div class="ai-head">
            <svg class="ai-spark" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2l1.6 6.4L20 10l-6.4 1.6L12 18l-1.6-6.4L4 10l6.4-1.6z" /></svg>
            <h4>Summary</h4>
            {#if selected.summary_model}<span class="model">local · {selected.summary_model}</span>{/if}
          </div>
          <ul>
            {#each summaryLines as p}<li>{p}</li>{/each}
          </ul>
        </aside>
      {/if}

      <div class="article-body">
        {#if selected.content_html}
          <!-- eslint-disable-next-line svelte/no-at-html-tags -->
          {@html selected.content_html}
        {:else if selected.content_text}
          <p>{selected.content_text}</p>
        {/if}
      </div>
    </div>

    {#if showShare}
      <ShareModal
        articleId={selected.id}
        articleTitle={selected.title}
        onClose={() => (showShare = false)}
      />
    {/if}
  {/if}
</section>

<style>
  .reader {
    overflow-y: auto;
    background: var(--paper);
    min-height: 0;
  }
  .reader-inner {
    max-width: 720px;
    margin: 0 auto;
    padding: 36px 52px 120px;
  }
  .reader-actions {
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 6px;
    background: linear-gradient(var(--paper) 70%, transparent);
    padding: 14px 26px;
    margin: 0 -52px 8px;
  }
  .ra-btn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 7px 12px;
    border-radius: 9px;
    font-size: 12.5px;
    font-weight: 600;
    color: var(--ink-soft);
    border: 1px solid var(--line);
    background: var(--card);
    transition: all 0.13s;
    text-decoration: none;
    font-family: inherit;
  }
  .ra-btn:hover { border-color: var(--ink-faint); color: var(--ink); }
  .ra-btn svg { width: 15px; height: 15px; }
  .ra-btn.starred { color: var(--gold); border-color: var(--gold); }
  .ra-btn.primary {
    background: var(--ember);
    color: #fff;
    border-color: var(--ember);
  }
  .ra-btn.primary:hover { background: var(--ember-soft); border-color: var(--ember-soft); color: #fff; }
  .board-wrap { position: relative; display: inline-flex; align-items: center; gap: 8px; }
  .board-picker {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 8px;
    box-shadow: var(--shadow-pane);
    padding: 4px;
    min-width: 180px;
    z-index: 20;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .board-picker button {
    text-align: left;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12.5px;
    color: var(--ink);
    background: transparent;
    border: none;
    cursor: pointer;
  }
  .board-picker button:hover { background: var(--line-soft); }
  .board-picker-empty {
    color: var(--ink-faint);
    font-size: 12px;
    padding: 8px 10px;
  }
  .board-msg {
    color: var(--ember);
    font-size: 12px;
    background: var(--ember-wash);
    padding: 2px 8px;
    border-radius: 4px;
  }

  .article-kicker {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 14px;
  }
  .article-kicker .favicon {
    width: 22px;
    height: 22px;
    border-radius: 6px;
    display: grid;
    place-items: center;
    color: #fff;
    font-size: 11px;
    font-weight: 800;
  }
  .article-kicker .src-name { font-weight: 700; font-size: 13px; color: var(--ink); }
  .article-kicker .src-time { color: var(--ink-faint); font-size: 12px; }
  .source-badge {
    font-size: 10px;
    font-weight: 800;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    padding: 2px 7px;
    border-radius: 5px;
    background: var(--line-soft);
    color: var(--ink-soft);
  }

  .article-h1 {
    font-family: var(--font-display);
    font-size: 32px;
    line-height: 1.08;
    font-weight: 500;
    letter-spacing: -0.02em;
    margin: 0 0 12px;
    color: var(--ink);
  }
  .article-hero {
    margin: 18px 0 24px;
    border-radius: 12px;
    overflow: hidden;
    background: var(--line-soft);
    aspect-ratio: 16 / 9;
    max-height: 360px;
  }
  .article-hero img { width: 100%; height: 100%; object-fit: cover; display: block; }

  .ai-card {
    border: 1px solid var(--line);
    background: linear-gradient(140deg, var(--ember-wash), var(--card) 70%);
    border-radius: 16px;
    padding: 18px 20px;
    margin: 22px 0 30px;
    position: relative;
    overflow: hidden;
  }
  :global([data-theme="dark"]) .ai-card {
    background: linear-gradient(140deg, var(--ember-wash), var(--card) 80%);
  }
  .ai-head {
    display: flex;
    align-items: center;
    gap: 9px;
    margin-bottom: 12px;
  }
  .ai-spark {
    width: 20px;
    height: 20px;
    color: var(--ember);
  }
  .ai-head h4 {
    font-family: var(--font-ui);
    font-size: 12px;
    font-weight: 800;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--ember);
    margin: 0;
  }
  .ai-head .model {
    margin-left: auto;
    font-size: 10.5px;
    color: var(--ink-faint);
    font-weight: 600;
    border: 1px solid var(--line);
    padding: 2px 8px;
    border-radius: 6px;
  }
  .ai-card ul {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 9px;
    margin: 0;
    padding: 0;
  }
  .ai-card li {
    font-family: var(--font-read);
    font-size: 14px;
    line-height: 1.45;
    color: var(--ink);
    padding-left: 18px;
    position: relative;
  }
  .ai-card li::before {
    content: "";
    position: absolute;
    left: 0;
    top: 9px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ember);
  }

  .article-body {
    font-family: var(--font-read);
    font-size: 17px;
    line-height: 1.6;
    color: var(--ink);
  }
  .article-body :global(p) { margin: 0 0 18px; }
  .article-body :global(p:first-of-type::first-letter) {
    font-family: var(--font-display);
    font-size: 52px;
    font-weight: 600;
    float: left;
    line-height: 0.82;
    margin: 6px 11px 0 0;
    color: var(--ember);
  }
  .article-body :global(h2) {
    font-family: var(--font-display);
    font-size: 20px;
    font-weight: 600;
    margin: 28px 0 10px;
    letter-spacing: -0.01em;
  }
  .article-body :global(blockquote) {
    border-left: 3px solid var(--ember);
    padding-left: 16px;
    margin: 22px 0;
    font-style: italic;
    color: var(--ink-soft);
  }
  .article-body :global(img) { max-width: 100%; height: auto; border-radius: 6px; margin: 12px 0; }
  .article-body :global(code) {
    font-family: ui-monospace, monospace;
    font-size: 0.9em;
    background: var(--line-soft);
    padding: 1px 5px;
    border-radius: 4px;
  }

  .reader-empty {
    display: grid;
    place-items: center;
    height: 100%;
    text-align: center;
    color: var(--ink-faint);
    padding: 40px;
  }
  .reader-empty .big {
    font-family: var(--font-display);
    font-size: 24px;
    color: var(--ink-soft);
    margin: 14px 0 6px;
  }
  .reader-empty svg { width: 50px; height: 50px; opacity: 0.4; }
</style>
