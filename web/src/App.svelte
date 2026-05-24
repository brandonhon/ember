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
    sidebarCollapsed,
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
  import ShortcutHelp from "./components/ShortcutHelp.svelte";
  import Settings from "./components/Settings.svelte";

  let keymapCleanup: () => void = () => {};
  let mounted = $state(false);
  let showHelp = $state(false);
  let showSettings = $state(false);

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
        showHelp = true;
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
    mounted = true;
  });

  onDestroy(() => {
    keymapCleanup();
    window.removeEventListener("ember:unauthorized", onUnauthorized);
  });

  // Whenever a user becomes authenticated (initial mount with valid session
  // OR after a successful login), populate the sidebar and article list.
  let loadedForUserId: number | null = $state(null);
  $effect(() => {
    if ($user && $user.id !== loadedForUserId) {
      loadedForUserId = $user.id;
      void refreshSidebar();
      void loadArticles(get(activeView));
    } else if (!$user) {
      loadedForUserId = null;
    }
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
    <TopBar onOpenSettings={() => (showSettings = true)} />
    <div class="panes" class:sidebar-collapsed={$sidebarCollapsed}>
      {#if !$sidebarCollapsed}
        <Sidebar />
      {/if}
      <ArticleList />
      <Reader />
    </div>
  </div>
  {#if showHelp}
    <ShortcutHelp onClose={() => (showHelp = false)} />
  {/if}
  {#if showSettings}
    <Settings onClose={() => (showSettings = false)} />
  {/if}
{/if}

<style>
  :global(:root) {
    /* Mockup palette — paper + ink + ember accents. */
    --paper: #f6f2e9;
    --paper-2: #efe9dc;
    --card: #fffdf8;
    --ink: #211d18;
    --ink-soft: #5b5347;
    --ink-faint: #8c8273;
    --line: #e2dac9;
    --line-soft: #ece5d6;
    --ember: #c2451d;
    --ember-soft: #e8643a;
    --ember-wash: #f6e2d8;
    --gold: #b07d1a;
    --green: #4f7a3d;
    --rail-w: 272px;
    --list-w: 380px;
    --topbar-h: 56px;
    --font-display: "Fraunces", Georgia, serif;
    --font-read: "Newsreader", Georgia, serif;
    --font-ui: "Bricolage Grotesque", system-ui, sans-serif;
    --shadow-card: 0 1px 2px rgba(33, 29, 24, 0.04), 0 8px 24px -16px rgba(33, 29, 24, 0.35);
    --shadow-pane: 0 0 0 1px var(--line), 0 24px 60px -32px rgba(33, 29, 24, 0.4);

    /* Legacy aliases — kept while components migrate. */
    --bg: var(--paper);
    --surface: var(--paper-2);
    --text: var(--ink);
    --muted: var(--ink-faint);
    --border: var(--line);
    --hover: var(--line-soft);
    --accent: var(--ember);
    --accent-bg: var(--ember-wash);
    --badge-bg: var(--line-soft);
  }
  :global(:root[data-theme="dark"]) {
    --paper: #15130f;
    --paper-2: #1c1915;
    --card: #211d18;
    --ink: #f0e9da;
    --ink-soft: #b8ac98;
    --ink-faint: #847a68;
    --line: #322c23;
    --line-soft: #2a2620;
    --ember: #e8643a;
    --ember-soft: #f0855f;
    --ember-wash: #2e1d15;
    --gold: #d2a23f;
    --green: #7fa766;
    --shadow-card: 0 1px 2px rgba(0, 0, 0, 0.3), 0 8px 24px -16px rgba(0, 0, 0, 0.8);
    --shadow-pane: 0 0 0 1px var(--line), 0 24px 60px -32px rgba(0, 0, 0, 0.9);
  }
  :global(*),
  :global(*::before),
  :global(*::after) {
    box-sizing: border-box;
  }
  :global(html),
  :global(body) { height: 100%; }
  :global(body) {
    margin: 0;
    background: var(--paper);
    color: var(--ink);
    font-family: var(--font-ui);
    -webkit-font-smoothing: antialiased;
    overflow: hidden;
    font-size: 14px;
    line-height: 1.45;
  }
  :global(button) {
    font-family: inherit;
    cursor: pointer;
    border: none;
    background: none;
    color: inherit;
    padding: 0;
  }
  :global(::selection) {
    background: var(--ember-wash);
    color: var(--ember);
  }
  :global(::-webkit-scrollbar) { width: 10px; height: 10px; }
  :global(::-webkit-scrollbar-thumb) {
    background: var(--line);
    border-radius: 10px;
    border: 3px solid transparent;
    background-clip: content-box;
  }
  :global(::-webkit-scrollbar-thumb:hover) {
    background: var(--ink-faint);
    background-clip: content-box;
  }

  .shell {
    display: grid;
    grid-template-rows: var(--topbar-h) 1fr;
    height: 100vh;
  }
  .panes {
    display: grid;
    grid-template-columns: var(--rail-w) var(--list-w) 1fr;
    min-height: 0;
    overflow: hidden;
  }
  .panes.sidebar-collapsed { grid-template-columns: var(--list-w) 1fr; }
  .boot {
    text-align: center;
    margin-top: 4rem;
    color: var(--ink-faint);
    font-family: var(--font-display);
    font-size: 18px;
  }
</style>
