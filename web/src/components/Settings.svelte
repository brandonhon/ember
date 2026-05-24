<script lang="ts">
  import {
    user,
    feverAPIKey,
    appVersion,
    theme,
    density,
    refreshMe,
    showSummary,
    showImages,
  } from "../lib/stores";
  import { api, ApiError, type StarterPack, type StarterImportResult } from "../lib/api";
  import { onMount } from "svelte";
  import { refreshSidebar } from "../lib/stores";
  import FilterManager from "./FilterManager.svelte";
  import ManageUsers from "./ManageUsers.svelte";

  let { onClose }: { onClose: () => void } = $props();

  type Section = "profile" | "preferences" | "mobile" | "filters" | "users" | "starter" | "about";
  let section = $state<Section>("profile");

  // Starter pack state ------------------------------------------------
  let starterPacks = $state<StarterPack[]>([]);
  let starterBusy = $state<string>("");
  let starterMsg = $state<string>("");
  let starterErr = $state<string>("");
  let starterLoaded = false;

  async function loadStarterPacks() {
    if (starterLoaded) return;
    try {
      const res = await api.listStarterPacks();
      starterPacks = res.data ?? [];
      starterLoaded = true;
    } catch (e) {
      starterErr = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function importStarter(slug: string) {
    starterBusy = slug;
    starterMsg = "";
    starterErr = "";
    try {
      const res = await api.importStarterPack(slug);
      const r: StarterImportResult = res.data;
      const parts: string[] = [];
      if (r.feeds_added) parts.push(`${r.feeds_added} added`);
      if (r.already_had) parts.push(`${r.already_had} already subscribed`);
      if (r.failed_urls?.length) parts.push(`${r.failed_urls.length} failed`);
      starterMsg = parts.join(" · ") || "Nothing to add";
      await refreshSidebar();
    } catch (e) {
      starterErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      starterBusy = "";
      setTimeout(() => (starterMsg = ""), 4000);
    }
  }

  $effect(() => {
    if (section === "starter") void loadStarterPacks();
  });

  onMount(() => {
    // Allow opening Settings directly to the starter pack section via hash.
    if (typeof location !== "undefined" && location.hash === "#starter") {
      section = "starter";
    }
  });

  // Password change state.
  let oldPassword = $state("");
  let newPassword = $state("");
  let confirmPassword = $state("");
  let pwBusy = $state(false);
  let pwMsg = $state("");
  let pwError = $state("");

  async function changePassword() {
    pwMsg = "";
    pwError = "";
    if (newPassword !== confirmPassword) {
      pwError = "passwords do not match";
      return;
    }
    if (newPassword.length < 8) {
      pwError = "new password must be at least 8 characters";
      return;
    }
    pwBusy = true;
    try {
      await api.changePassword(oldPassword, newPassword);
      pwMsg = "Password changed.";
      oldPassword = "";
      newPassword = "";
      confirmPassword = "";
    } catch (e) {
      pwError = e instanceof ApiError ? e.message : String(e);
    } finally {
      pwBusy = false;
      setTimeout(() => (pwMsg = ""), 4000);
    }
  }

  // Persist theme + density to localStorage so they survive reload.
  $effect(() => {
    if (typeof localStorage === "undefined") return;
    localStorage.setItem("ember:theme", $theme);
    localStorage.setItem("ember:density", $density);
  });

  function copyKey() {
    if (!$feverAPIKey) return;
    navigator.clipboard?.writeText($feverAPIKey).catch(() => {});
  }

  const feverURL = $derived(typeof location !== "undefined" ? `${location.origin}/fever` : "");

  // Toggling stays a child modal so we don't double-render filters/users.
  let showFilters = $state(false);
  let showUsers = $state(false);

  // Re-fetch /api/me on close so any changes (e.g. password) take effect.
  function onCloseAll() {
    refreshMe();
    onClose();
  }
</script>

<div
  class="backdrop"
  role="dialog"
  aria-modal="true"
  aria-labelledby="settings-title"
  on:click={onCloseAll}
  data-testid="settings"
>
  <div class="modal" on:click|stopPropagation>
    <header>
      <h2 id="settings-title">Settings</h2>
      <button class="close" on:click={onCloseAll} aria-label="Close">×</button>
    </header>

    <div class="layout">
      <nav class="nav" aria-label="Settings sections">
        <button class:active={section === "profile"} on:click={() => (section = "profile")} data-testid="settings-profile">Profile</button>
        <button class:active={section === "preferences"} on:click={() => (section = "preferences")}>Preferences</button>
        <button class:active={section === "mobile"} on:click={() => (section = "mobile")}>Mobile clients</button>
        <button class:active={section === "filters"} on:click={() => (section = "filters")}>Filters</button>
        <button class:active={section === "starter"} on:click={() => (section = "starter")} data-testid="settings-starter">Starter packs</button>
        {#if $user?.is_admin}
          <button class:active={section === "users"} on:click={() => (section = "users")} data-testid="settings-users">Users</button>
        {/if}
        <button class:active={section === "about"} on:click={() => (section = "about")}>About</button>
      </nav>

      <div class="content">
        {#if section === "profile"}
          <h3>Profile</h3>
          <div class="row">
            <label>
              <span>Username</span>
              <input type="text" value={$user?.username ?? ""} disabled />
            </label>
            <label>
              <span>Email</span>
              <input type="email" value={$user?.email ?? ""} disabled placeholder="not set" />
            </label>
          </div>
          <p class="hint">Email is managed by your administrator.</p>

          <h4>Change password</h4>
          {#if pwError}<p class="error" data-testid="pw-error">{pwError}</p>{/if}
          {#if pwMsg}<p class="ok" data-testid="pw-msg">{pwMsg}</p>{/if}
          <label>
            <span>Current password</span>
            <input type="password" bind:value={oldPassword} autocomplete="current-password" data-testid="pw-old" />
          </label>
          <label>
            <span>New password</span>
            <input type="password" bind:value={newPassword} autocomplete="new-password" data-testid="pw-new" />
          </label>
          <label>
            <span>Confirm new password</span>
            <input type="password" bind:value={confirmPassword} autocomplete="new-password" />
          </label>
          <div class="actions">
            <button on:click={changePassword} disabled={pwBusy || !oldPassword || !newPassword} data-testid="pw-submit">
              {pwBusy ? "Saving…" : "Change password"}
            </button>
          </div>
        {/if}

        {#if section === "preferences"}
          <h3>Preferences</h3>
          <div class="pref-row">
            <div>
              <div class="pref-label">Theme</div>
              <div class="pref-hint">Light or dark. Stored locally.</div>
            </div>
            <div class="seg">
              <button class:on={$theme === "light"} on:click={() => theme.set("light")}>Light</button>
              <button class:on={$theme === "dark"} on:click={() => theme.set("dark")}>Dark</button>
            </div>
          </div>
          <div class="pref-row">
            <div>
              <div class="pref-label">Article density</div>
              <div class="pref-hint">Cards have excerpts and actions. Compact shows just titles.</div>
            </div>
            <div class="seg">
              <button class:on={$density === "card"} on:click={() => density.set("card")}>Cards</button>
              <button class:on={$density === "compact"} on:click={() => density.set("compact")}>Compact</button>
            </div>
          </div>
          <div class="pref-row">
            <div>
              <div class="pref-label">AI summary card</div>
              <div class="pref-hint">When off, the article body is shown directly with no summary card.</div>
            </div>
            <div class="seg">
              <button class:on={$showSummary} on:click={() => showSummary.set(true)} data-testid="pref-summary-on">On</button>
              <button class:on={!$showSummary} on:click={() => showSummary.set(false)} data-testid="pref-summary-off">Off</button>
            </div>
          </div>
          <div class="pref-row">
            <div>
              <div class="pref-label">Article images</div>
              <div class="pref-hint">Show the main image inline. Images inside article body are kept regardless.</div>
            </div>
            <div class="seg">
              <button class:on={$showImages} on:click={() => showImages.set(true)} data-testid="pref-images-on">On</button>
              <button class:on={!$showImages} on:click={() => showImages.set(false)} data-testid="pref-images-off">Off</button>
            </div>
          </div>
        {/if}

        {#if section === "mobile"}
          <h3>Mobile clients</h3>
          <p class="hint">
            Reeder, FeedMe, and other Fever-compatible apps can connect using the URL and key
            below. The key is derived from your username and user ID — if it leaks, change your
            username via the admin.
          </p>
          <label>
            <span>Fever URL</span>
            <input type="text" value={feverURL} readonly />
          </label>
          <label>
            <span>API key</span>
            <input type="text" value={$feverAPIKey} readonly data-testid="fever-key" />
          </label>
          <div class="actions">
            <button on:click={copyKey} class="ghost">Copy key</button>
          </div>
        {/if}

        {#if section === "filters"}
          <h3>Filters</h3>
          <p class="hint">
            Rules applied to new articles as they arrive. Open the editor to add, disable, or
            delete filters.
          </p>
          <div class="actions">
            <button on:click={() => (showFilters = true)} data-testid="open-filters">Open filter editor</button>
          </div>
        {/if}

        {#if section === "starter"}
          <h3>Starter packs</h3>
          <p class="hint">Curated bundles of feeds. Click a pack to create the folder and subscribe — already-subscribed feeds are skipped.</p>
          {#if starterErr}<p class="error">{starterErr}</p>{/if}
          {#if starterMsg}<p class="ok" data-testid="starter-msg">{starterMsg}</p>{/if}
          <div class="pack-list">
            {#each starterPacks as p (p.slug)}
              <div class="pack">
                <div>
                  <div class="pack-name">
                    <span class="pack-dot" style="background:{p.color}"></span>
                    {p.name}
                  </div>
                  <div class="pack-hint">{p.feed_urls.length} feeds</div>
                </div>
                <button
                  on:click={() => importStarter(p.slug)}
                  disabled={starterBusy === p.slug}
                  data-testid="starter-import-{p.slug}"
                  class="pack-btn"
                >
                  {starterBusy === p.slug ? "Adding…" : "Add pack"}
                </button>
              </div>
            {/each}
          </div>
        {/if}

        {#if section === "users" && $user?.is_admin}
          <h3>Users</h3>
          <p class="hint">Admin-only. Add or remove user accounts.</p>
          <div class="actions">
            <button on:click={() => (showUsers = true)} data-testid="open-users">Manage users</button>
          </div>
        {/if}

        {#if section === "about"}
          <h3>About</h3>
          <dl class="kv">
            <dt>Version</dt><dd>{$appVersion}</dd>
            <dt>Project</dt><dd><a href="https://github.com/brandonhon/ember" target="_blank" rel="noopener noreferrer">github.com/brandonhon/ember</a></dd>
            <dt>License</dt><dd>private</dd>
          </dl>
        {/if}
      </div>
    </div>
  </div>

  {#if showFilters}
    <FilterManager onClose={() => (showFilters = false)} />
  {/if}
  {#if showUsers}
    <ManageUsers onClose={() => (showUsers = false)} />
  {/if}
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(33, 29, 24, 0.45);
    display: grid;
    place-items: center;
    z-index: 100;
    padding: 24px;
  }
  .modal {
    width: min(880px, 100%);
    /* Fixed height so the modal doesn't jitter as you switch sections
       with different content lengths. The inner .content scrolls. */
    height: min(640px, 86vh);
    overflow: hidden;
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 16px;
    box-shadow: var(--shadow-pane);
    display: flex;
    flex-direction: column;
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 22px;
    border-bottom: 1px solid var(--line);
  }
  h2 {
    margin: 0;
    font-family: var(--font-display);
    font-weight: 500;
    font-size: 20px;
  }
  .close {
    background: transparent;
    border: 0;
    font-size: 1.5rem;
    cursor: pointer;
    color: var(--ink-faint);
  }
  .close:hover { color: var(--ink); }
  .layout {
    display: grid;
    grid-template-columns: 180px 1fr;
    flex: 1;
    overflow: hidden;
  }
  .nav {
    background: var(--paper-2);
    border-right: 1px solid var(--line);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-y: auto;
  }
  .nav button {
    background: transparent;
    border: 0;
    text-align: left;
    padding: 8px 12px;
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 13px;
    color: var(--ink-soft);
    cursor: pointer;
  }
  .nav button:hover { background: var(--line-soft); color: var(--ink); }
  .nav button.active {
    background: var(--ember-wash);
    color: var(--ember);
    font-weight: 600;
  }
  .content { padding: 22px 26px; overflow-y: auto; }

  h3 {
    font-family: var(--font-display);
    font-size: 17px;
    font-weight: 500;
    margin: 0 0 12px;
    color: var(--ink);
  }
  h4 {
    font-size: 11.5px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--ink-faint);
    margin: 22px 0 8px;
  }
  .hint { color: var(--ink-faint); font-size: 12.5px; margin: 0 0 14px; line-height: 1.5; }
  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 12px;
    font-size: 12px;
  }
  label > span { color: var(--ink-faint); font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; font-size: 10.5px; }
  input[type="text"], input[type="password"], input[type="email"] {
    padding: 8px 11px;
    border: 1px solid var(--line);
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 13px;
    background: var(--paper);
    color: var(--ink);
  }
  input:focus { outline: none; border-color: var(--ember); box-shadow: 0 0 0 3px var(--ember-wash); }
  input[disabled], input[readonly] { color: var(--ink-soft); background: var(--paper-2); }
  .row { display: flex; gap: 12px; }
  .row > label { flex: 1; }
  .pref-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 12px 0;
    border-bottom: 1px solid var(--line-soft);
  }
  .pref-row:last-child { border-bottom: 0; }
  .pref-label { font-size: 13.5px; color: var(--ink); font-weight: 600; }
  .pref-hint { color: var(--ink-faint); font-size: 12px; margin-top: 2px; }
  .seg {
    display: inline-flex;
    border: 1px solid var(--line);
    border-radius: 20px;
    overflow: hidden;
    background: var(--card);
  }
  .seg button {
    padding: 5px 12px;
    font-size: 12px;
    font-weight: 600;
    color: var(--ink-faint);
    background: transparent;
    border: none;
    cursor: pointer;
  }
  .seg button.on { background: var(--ink); color: var(--paper); }
  .actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
  .actions button {
    background: var(--ember);
    color: #fff;
    border: none;
    padding: 7px 14px;
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
  }
  .actions button.ghost {
    background: transparent;
    color: var(--ink);
    border: 1px solid var(--line);
  }
  .actions button:hover:not(:disabled) { background: var(--ember-soft); }
  .actions button.ghost:hover { background: var(--line-soft); }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  .error {
    background: #fef2f2;
    color: #991b1b;
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 12.5px;
    margin-bottom: 10px;
  }
  .ok {
    background: var(--ember-wash);
    color: var(--ember);
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 12.5px;
    margin-bottom: 10px;
  }
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 6px 16px;
    font-size: 13px;
  }
  .kv dt { color: var(--ink-faint); font-weight: 600; }
  .kv dd { margin: 0; color: var(--ink); }
  .kv a { color: var(--ember); text-decoration: none; }
  .kv a:hover { text-decoration: underline; }

  .pack-list { display: flex; flex-direction: column; gap: 8px; margin-top: 6px; }
  .pack {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 14px;
    border: 1px solid var(--line);
    background: var(--card);
    border-radius: 10px;
  }
  .pack-name {
    font-size: 14px;
    font-weight: 600;
    color: var(--ink);
    display: inline-flex;
    align-items: center;
    gap: 9px;
  }
  .pack-dot {
    width: 10px;
    height: 10px;
    border-radius: 3px;
    display: inline-block;
  }
  .pack-hint { color: var(--ink-faint); font-size: 12px; margin-top: 3px; }
  .pack-btn {
    background: var(--ember);
    color: #fff;
    border: 0;
    padding: 6px 12px;
    border-radius: 7px;
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
  }
  .pack-btn:hover:not(:disabled) { background: var(--ember-soft); }
  .pack-btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
