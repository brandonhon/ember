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
    customPalette,
    refreshBranding,
    pollForNewArticles,
    newArticleCount,
    branding,
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
  function onOpenSettingsEvent() {
    showSettings = true;
  }

  // Auto-refresh: every 15s while the tab is visible, poll the active view
  // for new articles. The store prepends them and bumps newArticleCount,
  // which drives the favicon-dot indicator below. We also poll once on tab
  // re-focus so coming back from a long Slack rabbit hole feels instant.
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  function startPolling() {
    stopPolling();
    pollTimer = setInterval(() => {
      if (document.hidden) return;
      void pollForNewArticles();
    }, 15_000);
  }
  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }
  function onVisibility() {
    if (!document.hidden) void pollForNewArticles();
  }

  onMount(async () => {
    keymapCleanup = attach(handleAction);
    window.addEventListener("ember:unauthorized", onUnauthorized);
    window.addEventListener("ember:open-settings", onOpenSettingsEvent);
    document.addEventListener("visibilitychange", onVisibility);
    // Branding can be fetched without a session so the title/icon match the
    // server's identity even before the user logs in.
    void refreshBranding();
    await refreshMe();
    mounted = true;
  });

  onDestroy(() => {
    keymapCleanup();
    window.removeEventListener("ember:unauthorized", onUnauthorized);
    window.removeEventListener("ember:open-settings", onOpenSettingsEvent);
    document.removeEventListener("visibilitychange", onVisibility);
    stopPolling();
  });

  // Whenever a user becomes authenticated (initial mount with valid session
  // OR after a successful login), populate the sidebar, article list, and
  // begin polling. Stop polling when the user logs out.
  let loadedForUserId: number | null = $state(null);
  $effect(() => {
    if ($user && $user.id !== loadedForUserId) {
      loadedForUserId = $user.id;
      void refreshSidebar();
      void loadArticles(get(activeView));
      startPolling();
    } else if (!$user) {
      loadedForUserId = null;
      stopPolling();
    }
  });

  // Favicon dot: swap to icon-new.svg whenever we have unseen new articles.
  // Falls back to the branding favicon when the counter is 0. Works across
  // Chrome, Firefox, Edge, and Safari (all honor a runtime <link rel="icon">
  // href swap).
  $effect(() => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"][type="image/svg+xml"]');
    if (!link) return;
    link.href = $newArticleCount > 0 ? "/icon-new.svg" : ($branding.favicon_url || "/icon.svg");
  });

  // Also reflect the count in the page title prefix so it shows in the tab
  // text on browsers that crop the favicon (or in mobile tab strips).
  $effect(() => {
    const base = $branding.page_title || $branding.name || "Ember";
    document.title = $newArticleCount > 0 ? `(${$newArticleCount}) ${base}` : base;
  });

  // Theme application:
  //   - "auto" → resolve to "light" or "dark" via matchMedia.
  //   - any other value → applied as-is.
  // We also listen to OS theme changes when in auto mode so toggling dark
  // mode in macOS / Windows updates the page immediately.
  let osDark = $state(
    typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: dark)").matches
  );

  // Mobile breakpoint detection. Single-pane layout under 900px: only one of
  // sidebar/list/reader is visible at a time.
  let isMobile = $state(
    typeof window !== "undefined" && window.matchMedia?.("(max-width: 900px)").matches
  );
  $effect(() => {
    if (typeof window === "undefined") return;
    const m = window.matchMedia("(max-width: 900px)");
    const handler = (e: MediaQueryListEvent) => (isMobile = e.matches);
    m.addEventListener("change", handler);
    return () => m.removeEventListener("change", handler);
  });
  // Mobile sidebar drawer (open/closed) — separate from the desktop
  // collapse since desktop hides the rail entirely while mobile slides it
  // over the list.
  let mobileSidebarOpen = $state(false);
  // On mobile, selecting an article switches to reader-only. Tapping back
  // returns to the list. Drives via selectedArticleId.
  const mobilePane = $derived.by((): "list" | "reader" => {
    return $selectedArticleId !== null ? "reader" : "list";
  });
  function mobileBack() {
    selectedArticleId.set(null);
  }
  $effect(() => {
    if (typeof window === "undefined") return;
    const m = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = (e: MediaQueryListEvent) => (osDark = e.matches);
    m.addEventListener("change", handler);
    return () => m.removeEventListener("change", handler);
  });
  $effect(() => {
    const resolved = $theme === "auto" ? (osDark ? "dark" : "light") : $theme;
    document.documentElement.dataset.theme = resolved;
    // Persist the user's chosen value (not the resolved one).
    try { localStorage.setItem("ember:theme", $theme); } catch { /* ignore */ }
  });
  // Custom theme: apply the three user-picked colors as inline CSS variables
  // when the active theme is "custom". The static CSS block uses color-mix()
  // to derive the rest of the palette from these three.
  $effect(() => {
    if ($theme !== "custom") {
      document.documentElement.style.removeProperty("--paper");
      document.documentElement.style.removeProperty("--ink");
      document.documentElement.style.removeProperty("--ember");
      return;
    }
    document.documentElement.style.setProperty("--paper", $customPalette.paper);
    document.documentElement.style.setProperty("--ink", $customPalette.ink);
    document.documentElement.style.setProperty("--ember", $customPalette.ember);
  });
</script>

{#if !mounted}
  <p class="boot">Loading…</p>
{:else if !$user}
  <Login />
{:else}
  <div class="shell" data-theme={$theme} class:mobile={isMobile}>
    <TopBar
      onOpenSettings={() => (showSettings = true)}
      mobile={isMobile}
      onToggleMobileSidebar={() => (mobileSidebarOpen = !mobileSidebarOpen)}
      showBack={isMobile && mobilePane === "reader"}
      onBack={mobileBack}
    />
    <div
      class="panes"
      class:sidebar-collapsed={$sidebarCollapsed}
      class:mobile={isMobile}
      data-mobile-pane={mobilePane}
      class:drawer-open={isMobile && mobileSidebarOpen}
    >
      {#if isMobile}
        <!-- Mobile: sidebar is an off-canvas drawer; tap-outside closes it. -->
        {#if mobileSidebarOpen}
          <div class="mobile-scrim" on:click={() => (mobileSidebarOpen = false)} role="presentation"></div>
        {/if}
        <div class="mobile-drawer" class:open={mobileSidebarOpen}>
          <Sidebar />
        </div>
        {#if mobilePane === "list"}
          <ArticleList />
        {:else}
          <Reader />
        {/if}
      {:else}
        {#if !$sidebarCollapsed}
          <Sidebar />
        {/if}
        <ArticleList />
        <Reader />
      {/if}
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
    --ink-soft: #3f3930;
    --ink-faint: #6a604f;
    --line: #e2dac9;
    --line-soft: #ece5d6;
    --ember: #a93b16;
    --ember-soft: #e8643a;
    --ember-wash: #fbeae0;
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
  /* Solarized (light variant) — Ethan Schoonover's classic palette. */
  :global(:root[data-theme="solarized"]) {
    --paper: #fdf6e3;
    --paper-2: #eee8d5;
    --card: #fffbf0;
    --ink: #073642;
    --ink-soft: #495a62;
    --ink-faint: #657b83;
    --line: #e3dcc7;
    --line-soft: #f0ead2;
    --ember: #dc322f;
    --ember-soft: #cb4b16;
    --ember-wash: #fbe6cf;
    --gold: #b58900;
    --green: #859900;
  }
  /* Sepia — warm browns, e-reader friendly. */
  :global(:root[data-theme="sepia"]) {
    --paper: #f4e8d0;
    --paper-2: #ede0c5;
    --card: #f9efde;
    --ink: #3d2f1f;
    --ink-soft: #5b4a35;
    --ink-faint: #7a6849;
    --line: #d8c8aa;
    --line-soft: #e6d8b9;
    --ember: #8b4513;
    --ember-soft: #a0561a;
    --ember-wash: #ecd4b2;
    --gold: #9d7d2a;
    --green: #6b6232;
  }
  /* Nord — cool blue/gray dark theme. */
  :global(:root[data-theme="nord"]) {
    --paper: #2e3440;
    --paper-2: #3b4252;
    --card: #434c5e;
    --ink: #eceff4;
    --ink-soft: #d8dee9;
    --ink-faint: #88a3b3;
    --line: #4c566a;
    --line-soft: #3b4252;
    --ember: #d08770;
    --ember-soft: #ebcb8b;
    --ember-wash: #4c3a35;
    --gold: #ebcb8b;
    --green: #a3be8c;
    --shadow-card: 0 1px 2px rgba(0, 0, 0, 0.3), 0 8px 24px -16px rgba(0, 0, 0, 0.7);
    --shadow-pane: 0 0 0 1px var(--line), 0 24px 60px -32px rgba(0, 0, 0, 0.8);
  }
  /* Gruvbox (dark) — warm-tinted dark theme by morhetz. */
  :global(:root[data-theme="gruvbox"]) {
    --paper: #282828;
    --paper-2: #32302f;
    --card: #3c3836;
    --ink: #ebdbb2;
    --ink-soft: #d5c4a1;
    --ink-faint: #a89984;
    --line: #504945;
    --line-soft: #3c3836;
    --ember: #fe8019;
    --ember-soft: #fabd2f;
    --ember-wash: #4a3424;
    --gold: #fabd2f;
    --green: #b8bb26;
    --shadow-card: 0 1px 2px rgba(0, 0, 0, 0.4), 0 8px 24px -16px rgba(0, 0, 0, 0.85);
    --shadow-pane: 0 0 0 1px var(--line), 0 24px 60px -32px rgba(0, 0, 0, 0.9);
  }
  /* Custom — three core colors come from the user (set via inline style on
     :root by App.svelte); the rest are derived with color-mix() so palette
     coherence holds without making the user pick 13 hex codes. */
  :global(:root[data-theme="custom"]) {
    --paper-2: color-mix(in srgb, var(--paper) 92%, var(--ink) 8%);
    --card: color-mix(in srgb, var(--paper) 96%, white 4%);
    --ink-soft: color-mix(in srgb, var(--ink) 78%, var(--paper) 22%);
    --ink-faint: color-mix(in srgb, var(--ink) 55%, var(--paper) 45%);
    --line: color-mix(in srgb, var(--paper) 75%, var(--ink) 25%);
    --line-soft: color-mix(in srgb, var(--paper) 88%, var(--ink) 12%);
    --ember-soft: color-mix(in srgb, var(--ember) 78%, white 22%);
    --ember-wash: color-mix(in srgb, var(--ember) 14%, var(--paper) 86%);
    --gold: color-mix(in srgb, var(--ember) 60%, gold 40%);
    --green: color-mix(in srgb, var(--ember) 30%, green 70%);
  }
  /* High contrast — for WCAG AAA + low-vision users. */
  :global(:root[data-theme="contrast"]) {
    --paper: #000000;
    --paper-2: #0d0d0d;
    --card: #1a1a1a;
    --ink: #ffffff;
    --ink-soft: #f0f0f0;
    --ink-faint: #c8c8c8;
    --line: #ffffff;
    --line-soft: #2a2a2a;
    --ember: #ffd400;
    --ember-soft: #ffe04d;
    --ember-wash: #332a00;
    --gold: #ffd700;
    --green: #66ff66;
    --shadow-card: 0 1px 2px rgba(255, 255, 255, 0.15), 0 8px 24px -16px rgba(255, 255, 255, 0.3);
    --shadow-pane: 0 0 0 1px var(--line), 0 24px 60px -32px rgba(255, 255, 255, 0.25);
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

  /* Mobile single-pane layout. Sidebar slides in as a drawer on top of
     the visible pane; list and reader take turns at full width. */
  .panes.mobile { grid-template-columns: 1fr; position: relative; }
  .mobile-scrim {
    position: fixed;
    inset: var(--topbar-h) 0 0 0;
    background: rgba(33, 29, 24, 0.5);
    z-index: 30;
  }
  .mobile-drawer {
    position: fixed;
    top: var(--topbar-h);
    left: 0;
    bottom: 0;
    width: min(280px, 86vw);
    transform: translateX(-100%);
    transition: transform 0.18s ease;
    z-index: 40;
    background: var(--paper-2);
    border-right: 1px solid var(--line);
    overflow-y: auto;
  }
  .mobile-drawer.open { transform: translateX(0); }
  @media (max-width: 900px) {
    :global(:root) { --topbar-h: 52px; }
  }

  .boot {
    text-align: center;
    margin-top: 4rem;
    color: var(--ink-faint);
    font-family: var(--font-display);
    font-size: 18px;
  }
</style>
