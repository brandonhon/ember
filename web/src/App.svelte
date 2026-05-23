<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import {
    user,
    refreshMe,
    refreshSidebar,
    loadArticles,
    articles,
    selectedArticleId,
    activeView,
    theme,
    toggleStar,
    setRead,
  } from "./lib/stores";
  import { attach, type ShortcutAction } from "./lib/keyboard";
  import { get } from "svelte/store";
  import Login from "./components/Login.svelte";
  import Sidebar from "./components/Sidebar.svelte";
  import ArticleList from "./components/ArticleList.svelte";
  import Reader from "./components/Reader.svelte";
  import TopBar from "./components/TopBar.svelte";

  let keymapCleanup: () => void = () => {};
  let mounted = $state(false);

  function moveSelection(delta: number) {
    const items = get(articles).items;
    if (items.length === 0) return;
    const cur = get(selectedArticleId);
    const idx = cur === null ? -1 : items.findIndex((a) => a.id === cur);
    const next = Math.max(0, Math.min(items.length - 1, idx + delta));
    selectedArticleId.set(items[next].id);
  }

  function getSelected() {
    const id = get(selectedArticleId);
    if (id === null) return null;
    return get(articles).items.find((a) => a.id === id) ?? null;
  }

  function handleAction(action: ShortcutAction) {
    switch (action) {
      case "next":
        moveSelection(1);
        return;
      case "prev":
        moveSelection(-1);
        return;
      case "toggle-read": {
        const s = getSelected();
        if (s) setRead([s.id], !s.is_read);
        return;
      }
      case "toggle-star": {
        const s = getSelected();
        if (s) toggleStar(s.id, !s.is_starred);
        return;
      }
      case "open-original": {
        const s = getSelected();
        if (s?.url) window.open(s.url, "_blank", "noopener,noreferrer");
        return;
      }
      case "refresh":
        void refreshSidebar();
        void loadArticles(get(activeView));
        return;
      case "focus-search": {
        const el = document.querySelector<HTMLInputElement>(
          'input[data-testid="search-input"]',
        );
        el?.focus();
        return;
      }
      case "show-help":
        // TODO: shortcut overlay
        return;
    }
  }

  function onUnauthorized() {
    user.set(null);
  }

  onMount(async () => {
    keymapCleanup = attach(handleAction);
    window.addEventListener("ember:unauthorized", onUnauthorized);
    await refreshMe();
    if (get(user)) {
      await refreshSidebar();
      await loadArticles(get(activeView));
    }
    mounted = true;
  });

  onDestroy(() => {
    keymapCleanup();
    window.removeEventListener("ember:unauthorized", onUnauthorized);
  });

  $effect(() => {
    document.documentElement.dataset.theme = $theme;
  });
</script>

{#if !mounted}
  <p class="boot">Loading…</p>
{:else if !$user}
  <Login />
{:else}
  <div class="shell" data-theme={$theme}>
    <TopBar />
    <div class="panes">
      <Sidebar />
      <ArticleList />
      <Reader />
    </div>
  </div>
{/if}

<style>
  :global(:root) {
    --bg: #fdfaf6;
    --surface: #ffffff;
    --text: #1f2937;
    --muted: #6b7280;
    --border: #e5e7eb;
    --hover: #f3f4f6;
    --accent: #c2410c;
    --accent-bg: #fff7ed;
    --badge-bg: #f3f4f6;
  }
  :global(:root[data-theme="dark"]) {
    --bg: #0b0f17;
    --surface: #121821;
    --text: #e5e7eb;
    --muted: #9ca3af;
    --border: #1f2937;
    --hover: #1f2937;
    --accent: #fb923c;
    --accent-bg: rgba(251, 146, 60, 0.1);
    --badge-bg: #1f2937;
  }
  :global(body) {
    margin: 0;
    background: var(--bg);
    color: var(--text);
    font-family:
      "Inter",
      system-ui,
      -apple-system,
      sans-serif;
  }
  .shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
  }
  .panes {
    display: flex;
    flex: 1;
    overflow: hidden;
  }
  .boot {
    text-align: center;
    margin-top: 4rem;
    color: var(--muted);
  }
</style>
