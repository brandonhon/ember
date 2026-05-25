<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import {
    articles,
    boards,
    feeds,
    selectedArticleId,
    toggleStar,
    toggleLater,
    showSummary,
    showImages,
    summaryCollapsed,
  } from "../lib/stores";
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
  let showMutePicker = $state(false);
  let muteKeyword = $state("");
  let muteBusy = $state(false);
  // Per-article user tags. Loaded lazily when an article is selected.
  let articleTags = $state<string[]>([]);
  let tagInput = $state("");
  let tagBusy = $state(false);
  let tagsLoadedFor = $state<number | null>(null);

  async function loadTagsForSelected() {
    if (!selected) return;
    if (tagsLoadedFor === selected.id) return;
    tagsLoadedFor = selected.id;
    try {
      const res = await api.listArticleTags(selected.id);
      articleTags = res.data ?? [];
    } catch {
      articleTags = [];
    }
  }
  async function addTag() {
    const t = tagInput.trim();
    if (!t || !selected) return;
    tagBusy = true;
    try {
      const res = await api.addArticleTag(selected.id, t);
      articleTags = res.data ?? [];
      tagInput = "";
    } catch (err) {
      console.error("addTag", err);
    } finally {
      tagBusy = false;
    }
  }
  async function removeTag(t: string) {
    if (!selected) return;
    try {
      const res = await api.removeArticleTag(selected.id, t);
      articleTags = res.data ?? [];
    } catch (err) {
      console.error("removeTag", err);
    }
  }
  $effect(() => {
    void selected;
    if (selected) {
      void loadTagsForSelected();
    } else {
      articleTags = [];
      tagsLoadedFor = null;
    }
  });

  async function createMuteFilter() {
    const kw = muteKeyword.trim();
    if (!kw) return;
    muteBusy = true;
    try {
      await api.createFilter({
        name: `Mute: ${kw}`,
        match_json: JSON.stringify({ field: "title", op: "contains", value: kw }),
        action: "hide",
        enabled: true,
      });
      boardMsg = `Muting "${kw}" in titles`;
      muteKeyword = "";
      showMutePicker = false;
      setTimeout(() => (boardMsg = ""), 2400);
    } catch (err) {
      boardMsg = err instanceof ApiError ? err.message : String(err);
      setTimeout(() => (boardMsg = ""), 4000);
    } finally {
      muteBusy = false;
    }
  }

  function onDocClick(e: MouseEvent) {
    const t = e.target as HTMLElement;
    if (showBoardPicker && !t.closest("[data-board-picker]") && !t.closest("[data-board-trigger]")) {
      showBoardPicker = false;
    }
    if (showMutePicker && !t.closest("[data-mute-picker]") && !t.closest("[data-mute-trigger]")) {
      showMutePicker = false;
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
  // Reading time estimate at 200 wpm. Same logic as ArticleList.
  function readingMinutes(): number {
    if (!selected) return 0;
    const src = selected.content_text || (selected.content_html ? selected.content_html.replace(/<[^>]+>/g, " ") : "");
    if (!src) return 0;
    const words = src.trim().split(/\s+/).length;
    return Math.max(1, Math.round(words / 200));
  }

  // Stored summary format: "{paragraph...}\n\n• bullet 1\n• bullet 2\n• bullet 3"
  // The reader recovers paragraph + bullets by splitting on the first line that
  // starts with "• ". Legacy bullet-only summaries yield an empty paragraph.
  const summary = $derived.by(() => {
    if (!selected?.summary) return { paragraph: "", bullets: [] as string[] };
    const lines = selected.summary.split(/\n/);
    const firstBullet = lines.findIndex((l) => /^[•\-\*]\s+/.test(l.trim()));
    if (firstBullet < 0) {
      return { paragraph: selected.summary.trim(), bullets: [] as string[] };
    }
    const paragraph = lines.slice(0, firstBullet).join("\n").trim();
    const bullets = lines
      .slice(firstBullet)
      .map((s) => s.replace(/^\s*[•\-\*]\s*/, "").trim())
      .filter((s) => s.length > 0);
    return { paragraph, bullets };
  });

  function toggleSummary() {
    summaryCollapsed.update((v) => !v);
  }
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
        <div class="board-wrap">
          <button
            class="ra-btn"
            on:click={(e) => { e.stopPropagation(); showMutePicker = !showMutePicker; }}
            data-mute-trigger
            data-testid="reader-mute"
            title="Hide articles whose title contains a keyword"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 9v6h4l5 4V5L7 9H3z"/><line x1="22" y1="5" x2="14" y2="13"/><line x1="14" y1="5" x2="22" y2="13"/></svg>
            Mute
          </button>
          {#if showMutePicker}
            <div class="board-picker mute-picker" data-mute-picker>
              <input
                type="text"
                bind:value={muteKeyword}
                placeholder="keyword in title"
                on:keydown={(e) => { if (e.key === "Enter") createMuteFilter(); }}
                disabled={muteBusy}
                data-testid="mute-input"
              />
              <button
                on:click={createMuteFilter}
                disabled={muteBusy || !muteKeyword.trim()}
                data-testid="mute-submit"
              >
                {muteBusy ? "Muting…" : "Add mute rule"}
              </button>
              <div class="board-picker-empty">
                Future articles with this word in the title will be hidden. Manage all rules in Settings → Filters.
              </div>
            </div>
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
        {#if selected.dup_count > 0}
          <span class="dup-badge" title="Also published in other feeds you subscribe to">
            Also in {selected.dup_count + 1} feeds
          </span>
        {/if}
        <span class="src-time">· {timeAgo(selected.published_at)}</span>
        {#if readingMinutes() > 0}
          <span class="src-time">· {readingMinutes()} min read</span>
        {/if}
      </div>

      <h1 class="article-h1">{selected.title}</h1>

      <div class="tag-row" data-testid="article-tags">
        {#each articleTags as t (t)}
          <span class="tag-chip">
            #{t}
            <button class="tag-chip-x" on:click={() => removeTag(t)} aria-label={`Remove tag ${t}`} title={`Remove tag ${t}`}>×</button>
          </span>
        {/each}
        <input
          type="text"
          class="tag-input"
          placeholder={articleTags.length === 0 ? "Add tag…" : "+ tag"}
          bind:value={tagInput}
          on:keydown={(e) => { if (e.key === "Enter") addTag(); }}
          on:blur={() => tagInput.trim() && addTag()}
          disabled={tagBusy}
          data-testid="tag-input"
        />
      </div>

      {#if $showSummary && (summary.paragraph || summary.bullets.length > 0)}
        <aside class="ai-card" data-testid="summary-card">
          <header class="ai-head">
            <svg class="ai-spark" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2l1.6 6.4L20 10l-6.4 1.6L12 18l-1.6-6.4L4 10l6.4-1.6z" /></svg>
            <h4>Summary</h4>
            {#if selected.summary_model}<span class="model">local · {selected.summary_model}</span>{/if}
            <button class="ai-toggle" on:click={toggleSummary} aria-expanded={!$summaryCollapsed} aria-controls="ai-body" data-testid="summary-toggle">
              {$summaryCollapsed ? "Show" : "Hide"}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class:rot={$summaryCollapsed}><polyline points="6 9 12 15 18 9" /></svg>
            </button>
          </header>
          {#if !$summaryCollapsed}
            <div class="ai-body" id="ai-body">
              {#if $showImages && selected.image_url}
                <img class="ai-thumb" src={selected.image_url} alt="" loading="lazy" on:error={(e) => ((e.currentTarget as HTMLImageElement).style.display = "none")} />
              {/if}
              <div class="ai-text">
                {#if summary.paragraph}
                  {#each summary.paragraph.split(/\n{2,}/) as para}
                    <p class="ai-para">{para}</p>
                  {/each}
                {/if}
                {#if summary.bullets.length > 0}
                  <ul>
                    {#each summary.bullets as p}<li>{p}</li>{/each}
                  </ul>
                {/if}
              </div>
            </div>
          {/if}
        </aside>
      {/if}

      <div class="article-body">
        {#if selected.content_html}
          <!-- eslint-disable-next-line svelte/no-at-html-tags -->
          {@html selected.content_html}
        {:else if selected.cleaned_html}
          <!-- Original body missing — fall back to the LLM-cleaned text so
               the reader still has something to read. -->
          <!-- eslint-disable-next-line svelte/no-at-html-tags -->
          {@html selected.cleaned_html}
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
  /* Reader column grows with the pane (and gains width when the sidebar
     is collapsed), but caps for readability on very wide displays. Side
     padding scales with viewport: tight on narrow windows, more generous
     when there's room. */
  .reader-inner {
    max-width: min(1100px, 100%);
    margin: 0 auto;
    padding: 28px clamp(20px, 3vw, 56px) 120px;
  }
  @media (max-width: 900px) {
    .reader-inner { padding: 16px 16px 80px; }
    .article-h1 { font-size: 24px; }
  }
  .reader-actions {
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 6px;
    background: linear-gradient(var(--paper) 70%, transparent);
    /* Negative margin matches the parent's horizontal padding so the
       sticky bar bleeds edge-to-edge. */
    padding: 14px clamp(20px, 3vw, 56px);
    margin: 0 calc(-1 * clamp(20px, 3vw, 56px)) 8px;
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
  .mute-picker {
    padding: 10px;
    min-width: 220px;
    gap: 6px;
  }
  .mute-picker input[type="text"] {
    padding: 6px 9px;
    border: 1px solid var(--line);
    border-radius: 6px;
    font: inherit;
    font-size: 13px;
    background: var(--paper);
    color: var(--ink);
    width: 100%;
  }
  .mute-picker button {
    background: var(--ember);
    color: #fff;
    padding: 6px 10px;
    border-radius: 6px;
    font-weight: 600;
    font-size: 12px;
  }
  .mute-picker button:disabled { opacity: 0.5; cursor: not-allowed; }
  .mute-picker .board-picker-empty {
    font-size: 11px;
    padding: 4px 0 0;
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
  .dup-badge {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    padding: 2px 7px;
    border-radius: 5px;
    background: var(--paper-2);
    color: var(--ink-soft);
    border: 1px solid var(--line);
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
  .tag-row {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 0 0 16px;
    align-items: center;
  }
  .tag-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: var(--line-soft);
    color: var(--ink-soft);
    padding: 3px 4px 3px 9px;
    border-radius: 12px;
    font-family: var(--font-ui);
    font-size: 11.5px;
    font-weight: 600;
  }
  .tag-chip-x {
    background: transparent;
    border: 0;
    color: var(--ink-faint);
    cursor: pointer;
    font-size: 13px;
    line-height: 1;
    padding: 1px 4px;
    border-radius: 50%;
  }
  .tag-chip-x:hover { color: var(--ember); }
  .tag-input {
    background: transparent;
    border: 1px dashed var(--line);
    color: var(--ink-soft);
    font-family: var(--font-ui);
    font-size: 11.5px;
    font-weight: 600;
    padding: 3px 10px;
    border-radius: 12px;
    outline: none;
    width: 110px;
  }
  .tag-input:focus { border-style: solid; border-color: var(--ember); color: var(--ink); }
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
    padding: 22px 26px;
    margin: 24px 0 32px;
    position: relative;
    overflow: hidden;
    border-left: 4px solid var(--ember);
  }
  :global([data-theme="dark"]) .ai-card {
    background: linear-gradient(140deg, var(--ember-wash), var(--card) 80%);
  }
  .ai-head {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 14px;
  }
  .ai-spark {
    width: 22px;
    height: 22px;
    color: var(--ember);
  }
  .ai-head h4 {
    font-family: var(--font-ui);
    font-size: 13px;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--ember);
    margin: 0;
  }
  .ai-head .model {
    font-size: 10.5px;
    color: var(--ink-faint);
    font-weight: 600;
    border: 1px solid var(--line);
    padding: 2px 8px;
    border-radius: 6px;
  }
  .ai-toggle {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-family: var(--font-ui);
    font-size: 11.5px;
    font-weight: 700;
    color: var(--ink-soft);
    border: 1px solid var(--line);
    background: var(--card);
    border-radius: 7px;
    padding: 4px 9px;
  }
  .ai-toggle svg {
    width: 12px;
    height: 12px;
    transition: transform 0.18s ease;
  }
  .ai-toggle svg.rot { transform: rotate(-90deg); }
  .ai-toggle:hover { color: var(--ember); border-color: var(--ember); }

  .ai-body {
    display: grid;
    grid-template-columns: 1fr;
    gap: 16px;
  }
  .ai-body:has(.ai-thumb) {
    grid-template-columns: 200px 1fr;
  }
  .ai-thumb {
    width: 100%;
    height: 100%;
    max-height: 220px;
    object-fit: cover;
    border-radius: 10px;
    background: var(--line-soft);
  }
  .ai-text { min-width: 0; }
  .ai-para {
    font-family: var(--font-read);
    font-size: 16px;
    line-height: 1.55;
    color: var(--ink);
    margin: 0 0 12px;
  }
  .ai-para:last-child { margin-bottom: 0; }
  .ai-text ul {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin: 12px 0 0;
    padding: 0;
  }
  .ai-text li {
    font-family: var(--font-read);
    font-size: 15px;
    line-height: 1.5;
    color: var(--ink);
    padding-left: 20px;
    position: relative;
  }
  .ai-text li::before {
    content: "";
    position: absolute;
    left: 0;
    top: 9px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--ember);
  }
  @media (max-width: 720px) {
    .ai-body:has(.ai-thumb) { grid-template-columns: 1fr; }
    .ai-thumb { max-height: 180px; }
  }

  .article-body {
    font-family: var(--font-read);
    font-size: 17px;
    line-height: 1.6;
    color: var(--ink);
  }
  .article-body :global(p) { margin: 0 0 18px; }
  /* Drop cap: scoped to the FIRST direct-child paragraph only so nested
     <p> inside <ul>/<li>/<blockquote> don't get their own letter. Sized
     to fit within ~2 lines, and the subsequent direct-child paragraph
     clears the float so it doesn't get pushed right under the cap. */
  .article-body > :global(p:first-of-type::first-letter) {
    font-family: var(--font-display);
    font-size: 44px;
    font-weight: 600;
    float: left;
    line-height: 1;
    margin: 4px 9px 0 0;
    color: var(--ember);
  }
  .article-body > :global(p:first-of-type + p),
  .article-body > :global(p:first-of-type ~ *) {
    clear: left;
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
  /* Constrain any media or wrapper to the column width. Inline width
     attributes on <img>/<figure>/<picture> from publishers don't override
     max-width, but the wrapper itself can. So we cap both. */
  .article-body :global(img),
  .article-body :global(svg),
  .article-body :global(video),
  .article-body :global(iframe),
  .article-body :global(picture),
  .article-body :global(picture > img),
  .article-body :global(figure),
  .article-body :global(figure > img) {
    max-width: 100%;
    height: auto;
  }
  .article-body :global(img) { border-radius: 6px; margin: 12px 0; }
  .article-body :global(figure) { margin: 18px 0; }
  /* Wide tables get horizontal scroll instead of pushing the layout. */
  .article-body :global(table) {
    display: block;
    max-width: 100%;
    overflow-x: auto;
  }
  /* Last line of defense: never let any descendant force horizontal scroll
     on the column itself. */
  .article-body { overflow-wrap: break-word; word-break: break-word; }
  .article-body :global(*) { max-width: 100%; }
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
