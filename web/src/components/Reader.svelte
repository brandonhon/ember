<script lang="ts">
  import { articles, selectedArticleId, toggleStar, toggleLater } from "../lib/stores";
  import ShareModal from "./ShareModal.svelte";

  const selected = $derived(
    $selectedArticleId === null
      ? null
      : ($articles.items.find((a) => a.id === $selectedArticleId) ?? null),
  );

  let showShare = $state(false);
</script>

<section class="reader">
  {#if !selected}
    <div class="empty">Select an article to read.</div>
  {:else}
    <header>
      <h1>{selected.title}</h1>
      {#if selected.author}<p class="byline">{selected.author}</p>{/if}
      <div class="actions">
        <button on:click={() => toggleStar(selected.id, !selected.is_starred)} class:active={selected.is_starred} data-testid="reader-star">
          {selected.is_starred ? "★ Starred" : "☆ Star"}
        </button>
        <button on:click={() => toggleLater(selected.id, !selected.is_later)} class:active={selected.is_later}>
          {selected.is_later ? "✓ Saved for later" : "Save for later"}
        </button>
        <button on:click={() => (showShare = true)} data-testid="reader-share">
          Share
        </button>
        {#if selected.url}
          <a href={selected.url} target="_blank" rel="noopener noreferrer">Open original</a>
        {/if}
      </div>
    </header>

    {#if selected.summary}
      <aside class="summary" data-testid="summary-card">
        <h2>Summary</h2>
        <pre>{selected.summary}</pre>
      </aside>
    {/if}

    <article>
      {#if selected.content_html}
        <!-- eslint-disable-next-line svelte/no-at-html-tags -->
        {@html selected.content_html}
      {:else if selected.content_text}
        <p>{selected.content_text}</p>
      {/if}
    </article>

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
    flex: 2 1 480px;
    background: var(--surface);
    padding: 2rem;
    overflow-y: auto;
    font-family: "Newsreader", serif;
    line-height: 1.65;
    color: var(--text);
  }
  .empty { color: var(--muted); text-align: center; padding: 3rem; }
  header { margin-bottom: 1rem; border-bottom: 1px solid var(--border); padding-bottom: 1rem; }
  h1 { font-family: "Fraunces", serif; margin: 0 0 0.25rem; }
  .byline { color: var(--muted); margin: 0 0 0.75rem; }
  .actions { display: flex; gap: 0.5rem; flex-wrap: wrap; }
  .actions button, .actions a {
    background: transparent;
    border: 1px solid var(--border);
    padding: 0.3rem 0.7rem;
    border-radius: 4px;
    cursor: pointer;
    font: inherit;
    color: inherit;
    text-decoration: none;
  }
  .actions button.active { background: var(--accent-bg); border-color: var(--accent); color: var(--accent); }
  .summary {
    background: var(--accent-bg);
    border-left: 3px solid var(--accent);
    padding: 1rem;
    margin: 1rem 0;
    border-radius: 4px;
  }
  .summary h2 { font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.06em; margin: 0 0 0.5rem; }
  .summary pre { font: inherit; margin: 0; white-space: pre-wrap; }
  article { max-width: 65ch; }
  article :global(img) { max-width: 100%; height: auto; }
</style>
