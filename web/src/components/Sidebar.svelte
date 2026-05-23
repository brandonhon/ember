<script lang="ts">
  import {
    activeView,
    categories,
    feeds,
    totalUnread,
    loadArticles,
  } from "../lib/stores";

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
</script>

<aside class="sidebar">
  <section>
    <h3>Smart views</h3>
    <ul>
      <li><button on:click={() => pickSmart("fresh")} class:active={$activeView.kind === "smart" && $activeView.view === "fresh"} data-testid="view-fresh">Fresh</button></li>
      <li><button on:click={() => pickSmart("today")} class:active={$activeView.kind === "smart" && $activeView.view === "today"}>Today</button></li>
      <li><button on:click={() => pickSmart("unread")} class:active={$activeView.kind === "smart" && $activeView.view === "unread"}>All unread <span class="count">{$totalUnread}</span></button></li>
      <li><button on:click={() => pickSmart("starred")} class:active={$activeView.kind === "smart" && $activeView.view === "starred"}>Starred</button></li>
      <li><button on:click={() => pickSmart("later")} class:active={$activeView.kind === "smart" && $activeView.view === "later"}>Read later</button></li>
      <li><button on:click={() => pickSmart("shared")} class:active={$activeView.kind === "smart" && $activeView.view === "shared"}>Shared with me</button></li>
    </ul>
  </section>
  {#if $categories.length}
    <section>
      <h3>Folders</h3>
      <ul>
        {#each $categories as cat (cat.id)}
          <li>
            <button on:click={() => pickCategory(cat.id)} class:active={$activeView.kind === "category" && $activeView.id === cat.id}>
              {cat.name}
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}
  <section>
    <h3>Feeds</h3>
    <ul>
      {#each $feeds as f (f.id)}
        <li>
          <button on:click={() => pickFeed(f.id)} class:active={$activeView.kind === "feed" && $activeView.id === f.id} data-testid="feed-{f.id}">
            <span class="title">{f.title_override || f.title}</span>
            {#if f.unread > 0}
              <span class="count">{f.unread}</span>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  </section>
</aside>

<style>
  .sidebar {
    width: 260px;
    background: var(--surface);
    border-right: 1px solid var(--border);
    overflow-y: auto;
    padding: 0.5rem 0;
  }
  section { padding: 0.5rem 0.75rem; }
  h3 {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
    margin: 0.5rem 0 0.25rem;
  }
  ul { list-style: none; padding: 0; margin: 0; }
  button {
    width: 100%;
    text-align: left;
    background: transparent;
    border: 0;
    padding: 0.4rem 0.5rem;
    border-radius: 4px;
    cursor: pointer;
    color: inherit;
    font: inherit;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.5rem;
  }
  button:hover { background: var(--hover); }
  button.active { background: var(--accent-bg); color: var(--accent); }
  .count {
    font-size: 0.75rem;
    color: var(--muted);
    background: var(--badge-bg);
    padding: 0.05rem 0.4rem;
    border-radius: 999px;
    min-width: 1.5rem;
    text-align: center;
  }
  .title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
