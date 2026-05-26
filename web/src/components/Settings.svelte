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
    THEMES,
    customPalette,
    branding,
    refreshBranding,
    scrollMarksRead,
  } from "../lib/stores";
  import { api, ApiError, type StarterPack, type StarterImportResult, type LLMStatus, type DBStatus, type UserStats, type UserDigest, type PasskeySummary } from "../lib/api";
  import { createPasskey, passkeySupported } from "../lib/passkey";
  import { onMount } from "svelte";
  import { refreshSidebar } from "../lib/stores";
  import FilterManager from "./FilterManager.svelte";
  import ManageUsers from "./ManageUsers.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";

  type ConfirmReq = {
    title?: string;
    message: string;
    confirmLabel?: string;
    destructive?: boolean;
    run: () => Promise<void> | void;
  };
  let confirmReq = $state<ConfirmReq | null>(null);
  let confirmBusy = $state(false);
  async function runConfirm() {
    if (!confirmReq) return;
    confirmBusy = true;
    try {
      await confirmReq.run();
      confirmReq = null;
    } finally {
      confirmBusy = false;
    }
  }

  let { onClose }: { onClose: () => void } = $props();

  type Section = "profile" | "passkeys" | "preferences" | "stats" | "digest" | "mobile" | "filters" | "users" | "starter" | "llm" | "branding" | "database" | "about";
  let section = $state<Section>("profile");

  // Daily digest state -------------------------------------------------
  let digest = $state<UserDigest | null>(null);
  let digestErr = $state("");
  let digestMsg = $state("");
  let digestBusy = $state(false);
  async function loadDigest() {
    digestErr = "";
    try {
      const res = await api.getDigest();
      digest = res.data;
    } catch (e) {
      digestErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function saveDigest() {
    if (!digest) return;
    digestBusy = true;
    digestMsg = "";
    digestErr = "";
    try {
      const res = await api.setDigest(digest);
      digest = res.data;
      digestMsg = "Saved";
    } catch (e) {
      digestErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      digestBusy = false;
      setTimeout(() => (digestMsg = ""), 3000);
    }
  }

  // Reading stats state -----------------------------------------------
  let statsData = $state<UserStats | null>(null);
  let statsErr = $state("");
  async function loadStats() {
    statsErr = "";
    try {
      const res = await api.getStats();
      statsData = res.data;
    } catch (e) {
      statsErr = e instanceof ApiError ? e.message : String(e);
    }
  }

  // Database admin state ----------------------------------------------
  let dbState = $state<DBStatus | null>(null);
  let dbErr = $state("");
  let dbMsg = $state("");
  let dbBusy = $state("");
  let cleanupDays = $state(90);
  async function loadDB() {
    dbErr = "";
    try {
      const res = await api.getDBStatus();
      dbState = res.data;
      cleanupDays = res.data.cleanup_older_days || 90;
    } catch (e) {
      dbErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function runBackup() {
    dbBusy = "backup";
    dbMsg = "";
    dbErr = "";
    try {
      await api.dbBackup();
      await loadDB();
      dbMsg = "Backup created";
    } catch (e) {
      dbErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      dbBusy = "";
      setTimeout(() => (dbMsg = ""), 3000);
    }
  }
  function askCleanup() {
    confirmReq = {
      title: "Clean up old articles?",
      message: `Permanently delete articles older than ${cleanupDays} days that aren't starred, in a board, or saved for later. The database file is compacted afterwards.`,
      confirmLabel: "Clean up",
      destructive: true,
      run: () => runCleanup(),
    };
  }
  async function runCleanup() {
    dbBusy = "cleanup";
    dbMsg = "";
    dbErr = "";
    try {
      const res = await api.dbCleanup(cleanupDays);
      const { articles_deleted, bytes_reclaimed } = res.data;
      const mib = (bytes_reclaimed / (1024 * 1024)).toFixed(1);
      dbMsg = `Deleted ${articles_deleted} articles, reclaimed ${mib} MiB`;
      await loadDB();
    } catch (e) {
      dbErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      dbBusy = "";
      setTimeout(() => (dbMsg = ""), 5000);
    }
  }
  async function saveDBSchedule() {
    if (!dbState) return;
    dbBusy = "schedule";
    dbMsg = "";
    dbErr = "";
    try {
      await api.dbSchedule({
        backup_schedule: dbState.backup_schedule as "off" | "daily" | "weekly",
        backup_keep_count: dbState.backup_keep_count,
        cleanup_schedule: dbState.cleanup_schedule as "off" | "weekly" | "monthly",
        cleanup_older_days: dbState.cleanup_older_days,
        opml_schedule: (dbState.opml_schedule || "off") as "off" | "weekly" | "monthly",
      });
      dbMsg = "Schedule saved";
    } catch (e) {
      dbErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      dbBusy = "";
      setTimeout(() => (dbMsg = ""), 3000);
    }
  }
  function gibBytes(b: number): string {
    if (!b) return "—";
    const gb = b / (1024 * 1024 * 1024);
    if (gb >= 1) return gb.toFixed(2) + " GiB";
    const mb = b / (1024 * 1024);
    return mb.toFixed(1) + " MiB";
  }
  function fmtTime(unix: number): string {
    if (!unix) return "";
    return new Date(unix * 1000).toLocaleString();
  }

  // Branding admin state ----------------------------------------------
  let brandingDraft = $state({ name: "", page_title: "", favicon_url: "" });
  let brandingMsg = $state("");
  let brandingErr = $state("");
  let brandingBusy = $state(false);
  function loadBrandingDraft() {
    brandingDraft = { name: $branding.name, page_title: $branding.page_title, favicon_url: $branding.favicon_url };
  }
  async function saveBranding() {
    brandingBusy = true;
    brandingMsg = "";
    brandingErr = "";
    try {
      await api.setBranding(brandingDraft);
      await refreshBranding();
      brandingMsg = "Saved";
    } catch (e) {
      brandingErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      brandingBusy = false;
      setTimeout(() => (brandingMsg = ""), 3000);
    }
  }
  async function resetBranding() {
    brandingDraft = { name: "", page_title: "", favicon_url: "" };
    await saveBranding();
    loadBrandingDraft();
  }

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

  // LLM admin state ---------------------------------------------------
  let llm = $state<LLMStatus | null>(null);
  let llmErr = $state<string>("");
  let llmMsg = $state<string>("");
  let llmBusy = $state<string>(""); // active action: "switch:<model>", "pull:<model>", etc.
  let pullInput = $state<string>("");

  async function loadLLM() {
    llmErr = "";
    try {
      const res = await api.getLLMStatus();
      llm = res.data;
      if (!pullInput && llm?.recommended?.model) {
        pullInput = llm.recommended.model;
      }
    } catch (e) {
      llmErr = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function switchModel(name: string) {
    llmBusy = "switch:" + name;
    llmMsg = "";
    llmErr = "";
    try {
      await api.setLLMModel(name);
      llmMsg = `Now using ${name}`;
      await loadLLM();
    } catch (e) {
      llmErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      llmBusy = "";
      setTimeout(() => (llmMsg = ""), 3000);
    }
  }

  function askDeleteModel(name: string) {
    confirmReq = {
      title: "Delete model?",
      message: `Remove "${name}" from local storage. The model files are deleted from Ollama's cache.`,
      confirmLabel: "Delete",
      destructive: true,
      run: () => deleteModel(name),
    };
  }

  async function deleteModel(name: string) {
    llmBusy = "delete:" + name;
    llmMsg = "";
    llmErr = "";
    try {
      await api.deleteLLMModel(name);
      llmMsg = `Deleted ${name}`;
      await loadLLM();
    } catch (e) {
      llmErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      llmBusy = "";
      setTimeout(() => (llmMsg = ""), 3000);
    }
  }

  // LLM tuning state. Edited locally; submitted via Save button.
  let tuneTemp = $state(0);
  let tuneTopP = $state(0);
  let tuneCtx = $state(0);
  let tuneBusy = $state(false);
  let tuneMsg = $state("");

  function syncTuningFromLLM() {
    if (!llm) return;
    tuneTemp = llm.options?.temperature ?? 0;
    tuneTopP = llm.options?.top_p ?? 0;
    tuneCtx = llm.options?.num_ctx ?? 0;
  }
  async function saveTuning() {
    tuneBusy = true;
    llmErr = "";
    tuneMsg = "";
    try {
      await api.setLLMOptions({ temperature: Number(tuneTemp) || 0, top_p: Number(tuneTopP) || 0, num_ctx: Number(tuneCtx) || 0 });
      tuneMsg = "Saved";
      await loadLLM();
    } catch (e) {
      llmErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      tuneBusy = false;
      setTimeout(() => (tuneMsg = ""), 3000);
    }
  }
  $effect(() => {
    // Re-sync local sliders when the loaded llm state changes.
    syncTuningFromLLM();
  });

  async function pullModel() {
    const name = pullInput.trim();
    if (!name) return;
    llmBusy = "pull:" + name;
    llmMsg = "";
    llmErr = "";
    try {
      await api.pullLLMModel(name);
      llmMsg = `Pulled ${name}`;
      await loadLLM();
    } catch (e) {
      llmErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      llmBusy = "";
      setTimeout(() => (llmMsg = ""), 4000);
    }
  }

  function gib(bytes: number): string {
    if (!bytes) return "—";
    return (bytes / (1024 * 1024 * 1024)).toFixed(1) + " GiB";
  }
  function mib(bytes: number): string {
    if (!bytes) return "—";
    return (bytes / (1024 * 1024)).toFixed(0) + " MiB";
  }

  // Passkey admin state ----------------------------------------------
  const canPasskey = passkeySupported();
  let passkeys = $state<PasskeySummary[]>([]);
  let passkeyErr = $state("");
  let passkeyMsg = $state("");
  let passkeyBusy = $state<string>(""); // "register" | "delete:<id>"
  let newPasskeyName = $state("");
  async function loadPasskeys() {
    passkeyErr = "";
    try {
      const res = await api.listPasskeys();
      passkeys = res.data ?? [];
    } catch (e) {
      passkeyErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function addPasskey() {
    passkeyBusy = "register";
    passkeyErr = "";
    passkeyMsg = "";
    try {
      const begin = await api.passkeyRegisterBegin();
      const cred = await createPasskey(begin.data.options as any);
      const name = newPasskeyName.trim() || "Passkey";
      await api.passkeyRegisterFinish(begin.data.session_id, name, cred);
      newPasskeyName = "";
      passkeyMsg = "Passkey added";
      await loadPasskeys();
    } catch (e) {
      if (e instanceof ApiError) passkeyErr = e.message || "Registration failed";
      else if (e instanceof DOMException) passkeyErr = "Registration cancelled";
      else passkeyErr = String(e);
    } finally {
      passkeyBusy = "";
      setTimeout(() => (passkeyMsg = ""), 3000);
    }
  }
  function askDeletePasskey(p: PasskeySummary) {
    confirmReq = {
      title: "Remove passkey?",
      message: `Devices using "${p.name}" won't be able to sign in with it anymore.`,
      confirmLabel: "Remove",
      destructive: true,
      run: () => deletePasskey(p.id),
    };
  }
  async function deletePasskey(id: number) {
    passkeyBusy = "delete:" + id;
    passkeyErr = "";
    try {
      await api.deletePasskey(id);
      await loadPasskeys();
    } catch (e) {
      passkeyErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      passkeyBusy = "";
    }
  }

  $effect(() => {
    if (section === "starter") void loadStarterPacks();
    if (section === "llm" && $user?.is_admin) void loadLLM();
    if (section === "branding" && $user?.is_admin) loadBrandingDraft();
    if (section === "database" && $user?.is_admin) void loadDB();
    if (section === "stats") void loadStats();
    if (section === "digest") void loadDigest();
    if (section === "passkeys") void loadPasskeys();
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
        <button class:active={section === "passkeys"} on:click={() => (section = "passkeys")} data-testid="settings-passkeys">Passkeys</button>
        <button class:active={section === "preferences"} on:click={() => (section = "preferences")}>Preferences</button>
        <button class:active={section === "mobile"} on:click={() => (section = "mobile")}>Mobile clients</button>
        <button class:active={section === "filters"} on:click={() => (section = "filters")}>Filters</button>
        <button class:active={section === "stats"} on:click={() => (section = "stats")} data-testid="settings-stats">Reading stats</button>
        <button class:active={section === "digest"} on:click={() => (section = "digest")} data-testid="settings-digest">Daily digest</button>
        <button class:active={section === "starter"} on:click={() => (section = "starter")} data-testid="settings-starter">Starter packs</button>
        {#if $user?.is_admin}
          <button class:active={section === "llm"} on:click={() => (section = "llm")} data-testid="settings-llm">Language model</button>
          <button class:active={section === "branding"} on:click={() => (section = "branding")} data-testid="settings-branding">Branding</button>
          <button class:active={section === "database"} on:click={() => (section = "database")} data-testid="settings-database">Database</button>
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

        {#if section === "passkeys"}
          <h3>Passkeys</h3>
          {#if !canPasskey}
            <p class="hint">Your browser doesn't support passkeys.</p>
          {:else}
            <p class="hint">
              Passkeys let you sign in without a password using a fingerprint, face scan, or
              hardware key. Each device you register here can be used at sign-in.
            </p>

            {#if passkeyErr}<p class="error">{passkeyErr}</p>{/if}
            {#if passkeyMsg}<p class="ok">{passkeyMsg}</p>{/if}

            <h4>Add a passkey</h4>
            <label>
              <span>Name this device</span>
              <input
                type="text"
                bind:value={newPasskeyName}
                placeholder="e.g. MacBook Touch ID"
                maxlength="60"
              />
            </label>
            <div class="actions">
              <button
                on:click={addPasskey}
                disabled={passkeyBusy === "register"}
                data-testid="passkey-register"
              >
                {passkeyBusy === "register" ? "Waiting for device…" : "Register passkey"}
              </button>
            </div>

            <h4>Your passkeys</h4>
            {#if passkeys.length === 0}
              <p class="hint">No passkeys registered yet.</p>
            {:else}
              <ul class="list">
                {#each passkeys as p (p.id)}
                  <li class="list-row">
                    <div>
                      <div class="list-title">{p.name}</div>
                      <div class="list-sub">
                        Added {new Date(p.created_at * 1000).toLocaleDateString()}
                        {#if p.last_used_at}
                          · last used {new Date(p.last_used_at * 1000).toLocaleDateString()}
                        {/if}
                      </div>
                    </div>
                    <button
                      class="btn-danger"
                      on:click={() => askDeletePasskey(p)}
                      disabled={passkeyBusy === "delete:" + p.id}
                    >
                      Remove
                    </button>
                  </li>
                {/each}
              </ul>
            {/if}
          {/if}
        {/if}

        {#if section === "preferences"}
          <h3>Preferences</h3>
          <div class="pref-row">
            <div>
              <div class="pref-label">Theme</div>
              <div class="pref-hint">Auto matches your OS light/dark setting. Stored locally per-user.</div>
            </div>
            <div class="theme-grid">
              {#each THEMES as t (t.value)}
                <button
                  class="theme-tile"
                  class:on={$theme === t.value}
                  data-mood={t.mood}
                  on:click={() => theme.set(t.value)}
                  data-testid="theme-{t.value}"
                  aria-pressed={$theme === t.value}
                >
                  <span class="theme-swatches" data-theme-preview={t.value}>
                    <span class="sw paper"></span>
                    <span class="sw ink"></span>
                    <span class="sw ember"></span>
                  </span>
                  <span class="theme-label">{t.label}</span>
                </button>
              {/each}
            </div>
          </div>
          {#if $theme === "custom"}
            <div class="pref-row custom-editor">
              <div>
                <div class="pref-label">Custom palette</div>
                <div class="pref-hint">Pick three colors — the rest of the palette is derived automatically.</div>
              </div>
              <div class="color-pickers">
                <label>
                  <span>Background</span>
                  <input type="color" bind:value={$customPalette.paper} data-testid="custom-paper" />
                </label>
                <label>
                  <span>Text</span>
                  <input type="color" bind:value={$customPalette.ink} data-testid="custom-ink" />
                </label>
                <label>
                  <span>Accent</span>
                  <input type="color" bind:value={$customPalette.ember} data-testid="custom-ember" />
                </label>
              </div>
            </div>
          {/if}

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
              <div class="pref-label">Scroll-to-mark-read</div>
              <div class="pref-hint">Articles you scroll past in the list get marked read automatically.</div>
            </div>
            <div class="seg">
              <button class:on={$scrollMarksRead} on:click={() => scrollMarksRead.set(true)} data-testid="pref-scroll-on">On</button>
              <button class:on={!$scrollMarksRead} on:click={() => scrollMarksRead.set(false)} data-testid="pref-scroll-off">Off</button>
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

        {#if section === "stats"}
          <h3>Reading stats</h3>
          {#if statsErr}<p class="error">{statsErr}</p>{/if}
          {#if !statsData}
            <p class="muted">Loading…</p>
          {:else}
            <div class="stats-grid">
              <div class="stat-card">
                <div class="stat-num" data-testid="stat-today">{statsData.articles_read_today}</div>
                <div class="stat-label">Read today</div>
              </div>
              <div class="stat-card">
                <div class="stat-num">{statsData.articles_read_week}</div>
                <div class="stat-label">Read this week</div>
              </div>
              <div class="stat-card">
                <div class="stat-num">{statsData.articles_read_month}</div>
                <div class="stat-label">Read in 30 days</div>
              </div>
              <div class="stat-card">
                <div class="stat-num">{statsData.starred_total}</div>
                <div class="stat-label">Starred total</div>
              </div>
              <div class="stat-card">
                <div class="stat-num">{statsData.later_total}</div>
                <div class="stat-label">Read later</div>
              </div>
              <div class="stat-card">
                <div class="stat-num">{statsData.subscriptions}</div>
                <div class="stat-label">Subscriptions</div>
              </div>
            </div>
            {#if statsData.top_feeds && statsData.top_feeds.length > 0}
              <h4>Top feeds (last 30 days)</h4>
              <table class="llm-table">
                <thead><tr><th>Feed</th><th>Read</th></tr></thead>
                <tbody>
                  {#each statsData.top_feeds as f (f.feed_id)}
                    <tr><td>{f.title}</td><td>{f.read_count}</td></tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          {/if}
        {/if}

        {#if section === "digest"}
          <h3>Daily digest</h3>
          <p class="hint">Get an email at a fixed time each day with the articles in your chosen view. Requires the server to have SMTP configured.</p>
          {#if digestErr}<p class="error">{digestErr}</p>{/if}
          {#if digestMsg}<p class="ok" data-testid="digest-msg">{digestMsg}</p>{/if}
          {#if !digest}
            <p class="muted">Loading…</p>
          {:else}
            <div class="pref-row">
              <div>
                <div class="pref-label">Enabled</div>
                <div class="pref-hint">Sends one email per day. Skipped silently when no new articles match.</div>
              </div>
              <div class="seg">
                <button class:on={digest.enabled} on:click={() => (digest!.enabled = true)} data-testid="digest-on">On</button>
                <button class:on={!digest.enabled} on:click={() => (digest!.enabled = false)} data-testid="digest-off">Off</button>
              </div>
            </div>

            <label>
              <span>View</span>
              <select bind:value={digest.view_value} on:change={() => (digest!.view_kind = "smart")} data-testid="digest-view">
                <option value="fresh">Fresh (last 24h)</option>
                <option value="today">Today</option>
                <option value="unread">All unread</option>
                <option value="starred">Starred</option>
                <option value="later">Read later</option>
              </select>
              <span class="pref-hint">Saved searches, feeds and folders can be wired in later; smart views are the common case.</span>
            </label>

            <div class="rec-row">
              <label class="inline-label">
                <span>Hour (UTC)</span>
                <input type="number" min="0" max="23" bind:value={digest.hour_utc} data-testid="digest-hour" />
              </label>
              <label class="inline-label">
                <span>Minute (UTC)</span>
                <input type="number" min="0" max="59" bind:value={digest.minute_utc} data-testid="digest-minute" />
              </label>
            </div>

            <label>
              <span>Email override</span>
              <input type="email" bind:value={digest.email_override} placeholder="optional — defaults to your account email" data-testid="digest-email" />
            </label>

            <div class="actions">
              <button on:click={saveDigest} disabled={digestBusy} data-testid="digest-save">
                {digestBusy ? "Saving…" : "Save"}
              </button>
            </div>
          {/if}
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

        {#if section === "llm" && $user?.is_admin}
          <h3>Language model</h3>
          <p class="hint">Switch models or pull new ones from Ollama. The recommendation matches what fits your host.</p>
          {#if llmErr}<p class="error" data-testid="llm-error">{llmErr}</p>{/if}
          {#if llmMsg}<p class="ok" data-testid="llm-msg">{llmMsg}</p>{/if}
          {#if !llm}
            <p class="muted">Loading…</p>
          {:else if !llm.enabled}
            <p class="muted">Summaries are disabled on this server (EMBER_DISABLE_SUMMARIES=1).</p>
          {:else}
            <h4>This host</h4>
            <dl class="kv">
              <dt>RAM</dt><dd>{gib(llm.system.ram_bytes)}</dd>
              <dt>CPUs</dt><dd>{llm.system.cpus}</dd>
              <dt>GPU</dt><dd>{llm.system.gpu || "none detected"}</dd>
              <dt>OS</dt><dd>{llm.system.os}</dd>
            </dl>
            <h4>Recommendation</h4>
            <div class="rec-row">
              <div>
                <div class="pref-label">{llm.recommended.disable_llm ? "Disable summaries" : llm.recommended.model}</div>
                <div class="pref-hint">{llm.recommended.reason}</div>
              </div>
              {#if !llm.recommended.disable_llm && llm.recommended.model !== llm.current_model}
                <button
                  class="pack-btn"
                  on:click={() => switchModel(llm!.recommended.model)}
                  disabled={llmBusy.startsWith("switch:") || llmBusy.startsWith("pull:")}
                  data-testid="llm-use-recommended"
                >
                  Use this
                </button>
              {/if}
            </div>

            <h4>Active model</h4>
            <p data-testid="llm-current"><strong>{llm.current_model || "(none)"}</strong></p>

            <h4>Installed</h4>
            {#if llm.installed_err}
              <p class="error">Couldn't list installed models: {llm.installed_err}</p>
            {:else if !llm.installed || llm.installed.length === 0}
              <p class="muted">No models installed yet. Pull one below.</p>
            {:else}
              <table class="llm-table">
                <thead>
                  <tr><th>Name</th><th>Size</th><th></th></tr>
                </thead>
                <tbody>
                  {#each llm.installed as m (m.name)}
                    <tr>
                      <td><code>{m.name}</code></td>
                      <td>{mib(m.size_bytes)}</td>
                      <td class="llm-actions">
                        {#if m.name === llm.current_model}
                          <span class="muted">active</span>
                        {:else}
                          <button
                            class="pack-btn"
                            on:click={() => switchModel(m.name)}
                            disabled={llmBusy !== ""}
                            data-testid="llm-switch-{m.name}"
                          >
                            {llmBusy === "switch:" + m.name ? "Switching…" : "Use"}
                          </button>
                          <button
                            class="ghost-btn"
                            on:click={() => askDeleteModel(m.name)}
                            disabled={llmBusy !== ""}
                            data-testid="llm-delete-{m.name}"
                          >
                            {llmBusy === "delete:" + m.name ? "Deleting…" : "Delete"}
                          </button>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}

            <h4>Tuning</h4>
            <p class="hint">Generation parameters passed to Ollama. 0 means "use the model default".</p>
            {#if tuneMsg}<p class="ok" data-testid="tune-msg">{tuneMsg}</p>{/if}
            <div class="tune-row">
              <label>
                <span class="tune-label">Temperature <em>{(+tuneTemp).toFixed(2)}</em></span>
                <input type="range" min="0" max="2" step="0.05" bind:value={tuneTemp} data-testid="tune-temp" />
                <span class="tune-hint">0 = deterministic, 1 = default, &gt;1 = creative</span>
              </label>
              <label>
                <span class="tune-label">Top P <em>{(+tuneTopP).toFixed(2)}</em></span>
                <input type="range" min="0" max="1" step="0.05" bind:value={tuneTopP} data-testid="tune-top-p" />
                <span class="tune-hint">Lower = focused, higher = diverse</span>
              </label>
              <label>
                <span class="tune-label">Context window <em>{tuneCtx || "default"}</em></span>
                <input type="range" min="0" max="16384" step="512" bind:value={tuneCtx} data-testid="tune-ctx" />
                <span class="tune-hint">Max tokens the model considers. 0 = model default (usually 2048)</span>
              </label>
            </div>
            <div class="actions">
              <button on:click={saveTuning} disabled={tuneBusy} data-testid="tune-save">
                {tuneBusy ? "Saving…" : "Save tuning"}
              </button>
            </div>

            <h4>Pull a new model</h4>
            <p class="hint">e.g. <code>qwen2.5:0.5b</code>, <code>qwen2.5:1.5b</code>, <code>llama3.2:1b</code>. Downloads can take several minutes.</p>
            <div class="rec-row">
              <input
                type="text"
                bind:value={pullInput}
                placeholder="qwen2.5:0.5b"
                data-testid="llm-pull-input"
              />
              <button
                class="pack-btn"
                on:click={pullModel}
                disabled={!pullInput.trim() || llmBusy.startsWith("pull:")}
                data-testid="llm-pull-submit"
              >
                {llmBusy.startsWith("pull:") ? "Pulling…" : "Pull"}
              </button>
            </div>
          {/if}
        {/if}

        {#if section === "branding" && $user?.is_admin}
          <h3>Branding</h3>
          <p class="hint">Change the app name, browser tab title, and favicon shown to all users. Leave a field blank to restore the default.</p>
          {#if brandingErr}<p class="error">{brandingErr}</p>{/if}
          {#if brandingMsg}<p class="ok" data-testid="branding-msg">{brandingMsg}</p>{/if}
          <label>
            <span>App name</span>
            <input type="text" bind:value={brandingDraft.name} placeholder="Ember" data-testid="branding-name" />
          </label>
          <label>
            <span>Browser tab title</span>
            <input type="text" bind:value={brandingDraft.page_title} placeholder="Ember Reader" data-testid="branding-title" />
          </label>
          <label>
            <span>Favicon URL</span>
            <input type="text" bind:value={brandingDraft.favicon_url} placeholder="/favicon.svg or data:image/svg+xml;..." data-testid="branding-favicon" />
            <span class="pref-hint">Public URL (e.g. /favicon.svg, https://…/icon.png) or a data: URI. Hard-refresh after changing.</span>
          </label>
          <div class="actions">
            <button class="ghost" on:click={resetBranding} disabled={brandingBusy}>Reset to defaults</button>
            <button on:click={saveBranding} disabled={brandingBusy} data-testid="branding-save">
              {brandingBusy ? "Saving…" : "Save"}
            </button>
          </div>
        {/if}

        {#if section === "database" && $user?.is_admin}
          <h3>Database</h3>
          {#if dbErr}<p class="error">{dbErr}</p>{/if}
          {#if dbMsg}<p class="ok" data-testid="db-msg">{dbMsg}</p>{/if}
          {#if !dbState}
            <p class="muted">Loading…</p>
          {:else}
            <h4>Status</h4>
            <dl class="kv">
              <dt>Size on disk</dt><dd>{gibBytes(dbState.size_bytes)}</dd>
              <dt>Page count</dt><dd>{dbState.page_count.toLocaleString()}</dd>
              <dt>Backup directory</dt><dd><code>{dbState.backup_dir}</code></dd>
            </dl>

            <h4>Manual backup</h4>
            <p class="hint">Writes a compacted snapshot to <code>{dbState.backup_dir}</code>. Safe to run while ember is serving.</p>
            <div class="actions">
              <button on:click={runBackup} disabled={dbBusy === "backup"} data-testid="db-backup">
                {dbBusy === "backup" ? "Backing up…" : "Back up now"}
              </button>
            </div>

            {#if (dbState.backups?.length ?? 0) > 0}
              <h4>Recent backups</h4>
              <table class="llm-table">
                <thead><tr><th>File</th><th>Size</th><th>Created</th></tr></thead>
                <tbody>
                  {#each (dbState.backups ?? []).slice(0, 8) as b (b.path)}
                    <tr>
                      <td><code>{b.path.split("/").slice(-1)[0]}</code></td>
                      <td>{gibBytes(b.size_bytes)}</td>
                      <td>{fmtTime(b.created_at)}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}

            <h4>Manual cleanup</h4>
            <p class="hint">Delete articles older than the chosen window that aren't starred, in a board, or saved for later. Compacts the file afterwards.</p>
            <div class="rec-row">
              <label class="inline-label">
                <span>Older than (days)</span>
                <input type="number" min="7" max="3650" bind:value={cleanupDays} data-testid="db-cleanup-days" />
              </label>
              <button class="ghost-btn" on:click={askCleanup} disabled={dbBusy === "cleanup"} data-testid="db-cleanup">
                {dbBusy === "cleanup" ? "Cleaning…" : "Clean up now"}
              </button>
            </div>

            <h4>Schedule</h4>
            <p class="hint">Automatic backups and cleanup, run by a background job. Tick every hour; missed runs catch up on the next tick.</p>
            <div class="pref-row">
              <div>
                <div class="pref-label">Backup</div>
                <div class="pref-hint">Keep the {dbState.backup_keep_count} most recent.</div>
              </div>
              <div class="seg">
                <button class:on={dbState.backup_schedule === "off"} on:click={() => (dbState!.backup_schedule = "off")}>Off</button>
                <button class:on={dbState.backup_schedule === "daily"} on:click={() => (dbState!.backup_schedule = "daily")}>Daily</button>
                <button class:on={dbState.backup_schedule === "weekly"} on:click={() => (dbState!.backup_schedule = "weekly")}>Weekly</button>
              </div>
            </div>
            <div class="pref-row">
              <div>
                <div class="pref-label">Cleanup</div>
                <div class="pref-hint">Older than {dbState.cleanup_older_days} days, when scheduled.</div>
              </div>
              <div class="seg">
                <button class:on={dbState.cleanup_schedule === "off"} on:click={() => (dbState!.cleanup_schedule = "off")}>Off</button>
                <button class:on={dbState.cleanup_schedule === "weekly"} on:click={() => (dbState!.cleanup_schedule = "weekly")}>Weekly</button>
                <button class:on={dbState.cleanup_schedule === "monthly"} on:click={() => (dbState!.cleanup_schedule = "monthly")}>Monthly</button>
              </div>
            </div>
            <div class="pref-row">
              <div>
                <div class="pref-label">OPML export</div>
                <div class="pref-hint">Writes the admin user's subscription list to /data/exports/ on the chosen cadence.</div>
              </div>
              <div class="seg">
                <button class:on={(dbState.opml_schedule || "off") === "off"} on:click={() => (dbState!.opml_schedule = "off")}>Off</button>
                <button class:on={dbState.opml_schedule === "weekly"} on:click={() => (dbState!.opml_schedule = "weekly")}>Weekly</button>
                <button class:on={dbState.opml_schedule === "monthly"} on:click={() => (dbState!.opml_schedule = "monthly")}>Monthly</button>
              </div>
            </div>
            <div class="rec-row">
              <label class="inline-label">
                <span>Keep N backups</span>
                <input type="number" min="1" max="365" bind:value={dbState.backup_keep_count} data-testid="db-keep" />
              </label>
              <label class="inline-label">
                <span>Cleanup window (days)</span>
                <input type="number" min="7" max="3650" bind:value={dbState.cleanup_older_days} data-testid="db-cleanup-days-sched" />
              </label>
            </div>
            <div class="actions">
              <button on:click={saveDBSchedule} disabled={dbBusy === "schedule"} data-testid="db-schedule-save">
                {dbBusy === "schedule" ? "Saving…" : "Save schedule"}
              </button>
            </div>
          {/if}
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
  {#if confirmReq}
    <ConfirmDialog
      title={confirmReq.title}
      message={confirmReq.message}
      confirmLabel={confirmReq.confirmLabel ?? "Confirm"}
      destructive={confirmReq.destructive ?? false}
      busy={confirmBusy}
      onConfirm={runConfirm}
      onCancel={() => (confirmReq = null)}
    />
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

  /* Theme grid: tiles with three-stripe color preview each. The .swatches
     inner spans render per-theme via [data-theme-preview="..."] selectors so
     the preview matches the actual palette. */
  .theme-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    gap: 8px;
    margin-top: 8px;
    flex-basis: 100%;
  }
  .pref-row:has(.theme-grid) {
    flex-direction: column;
    align-items: stretch;
  }
  .theme-tile {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    background: var(--card);
    border-radius: 10px;
    cursor: pointer;
    transition: border-color 0.12s;
  }
  .theme-tile:hover { border-color: var(--ink-faint); }
  .theme-tile.on { border-color: var(--ember); box-shadow: 0 0 0 2px var(--ember-wash); }
  .theme-swatches {
    display: flex;
    gap: 0;
    height: 28px;
    border-radius: 6px;
    overflow: hidden;
    border: 1px solid var(--line);
  }
  .theme-swatches .sw {
    flex: 1;
  }
  .theme-label {
    font-family: var(--font-ui);
    font-size: 12px;
    font-weight: 600;
    color: var(--ink-soft);
    text-align: left;
  }
  .theme-tile.on .theme-label { color: var(--ember); }
  /* Preview palettes — kept in sync with App.svelte. Note that "auto" gets
     the light preview since it's the most common case. */
  [data-theme-preview="auto"]  .paper { background: #f6f2e9; } [data-theme-preview="auto"]  .ink { background: #211d18; } [data-theme-preview="auto"]  .ember { background: #a93b16; }
  [data-theme-preview="light"] .paper { background: #f6f2e9; } [data-theme-preview="light"] .ink { background: #211d18; } [data-theme-preview="light"] .ember { background: #a93b16; }
  [data-theme-preview="dark"]  .paper { background: #15130f; } [data-theme-preview="dark"]  .ink { background: #f0e9da; } [data-theme-preview="dark"]  .ember { background: #e8643a; }
  [data-theme-preview="solarized"] .paper { background: #fdf6e3; } [data-theme-preview="solarized"] .ink { background: #073642; } [data-theme-preview="solarized"] .ember { background: #dc322f; }
  [data-theme-preview="sepia"] .paper { background: #f4e8d0; } [data-theme-preview="sepia"] .ink { background: #3d2f1f; } [data-theme-preview="sepia"] .ember { background: #8b4513; }
  [data-theme-preview="nord"]  .paper { background: #2e3440; } [data-theme-preview="nord"]  .ink { background: #eceff4; } [data-theme-preview="nord"]  .ember { background: #d08770; }
  [data-theme-preview="gruvbox"] .paper { background: #282828; } [data-theme-preview="gruvbox"] .ink { background: #ebdbb2; } [data-theme-preview="gruvbox"] .ember { background: #fe8019; }
  [data-theme-preview="contrast"] .paper { background: #000000; } [data-theme-preview="contrast"] .ink { background: #ffffff; } [data-theme-preview="contrast"] .ember { background: #ffd400; }
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

  .rec-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 12px 14px;
    border: 1px solid var(--line);
    background: var(--card);
    border-radius: 10px;
    margin-bottom: 10px;
  }
  .rec-row input[type="text"],
  .rec-row input[type="number"] {
    flex: 1;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    font: inherit;
    font-size: 13px;
    background: var(--paper);
    color: var(--ink);
  }
  .inline-label {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: 1;
    margin: 0;
  }
  .inline-label > span {
    font-size: 10.5px;
    color: var(--ink-faint);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    gap: 10px;
    margin: 6px 0 14px;
  }
  .stat-card {
    padding: 14px 16px;
    border: 1px solid var(--line);
    background: var(--card);
    border-radius: 10px;
  }
  .stat-num {
    font-family: var(--font-display);
    font-size: 28px;
    font-weight: 500;
    color: var(--ember);
    line-height: 1;
  }
  .stat-label {
    font-size: 11.5px;
    color: var(--ink-faint);
    margin-top: 4px;
    font-weight: 600;
  }
  .llm-table {
    width: 100%;
    border-collapse: collapse;
    margin: 6px 0 16px;
    font-size: 13px;
  }
  .llm-table th, .llm-table td {
    text-align: left;
    padding: 8px 10px;
    border-bottom: 1px solid var(--line-soft);
  }
  .llm-table th {
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--ink-faint);
    font-weight: 700;
  }
  .llm-table code {
    font-family: ui-monospace, monospace;
    font-size: 12px;
    background: var(--line-soft);
    padding: 1px 5px;
    border-radius: 4px;
  }
  .llm-actions { display: flex; gap: 6px; align-items: center; justify-content: flex-end; }
  .ghost-btn {
    background: transparent;
    color: var(--ink-soft);
    border: 1px solid var(--line);
    padding: 5px 10px;
    border-radius: 7px;
    font: inherit;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
  }
  .ghost-btn:hover:not(:disabled) { color: #b3261e; border-color: #b3261e; }
  .ghost-btn:disabled { opacity: 0.5; cursor: not-allowed; }

  .custom-editor { flex-direction: column; align-items: stretch; }
  .color-pickers {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
    margin-top: 6px;
    flex-basis: 100%;
  }
  .color-pickers label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
  }
  .color-pickers label > span {
    font-size: 11px;
    color: var(--ink-faint);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .color-pickers input[type="color"] {
    width: 100%;
    height: 38px;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 3px;
    background: var(--card);
    cursor: pointer;
  }

  .tune-row {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 6px 0 12px;
  }
  .tune-row label {
    display: grid;
    grid-template-columns: 1fr auto;
    grid-template-areas: "label value" "range range" "hint hint";
    gap: 4px 12px;
    margin: 0;
  }
  .tune-label {
    grid-area: label;
    font-size: 12.5px;
    font-weight: 600;
    color: var(--ink);
    text-transform: none;
    letter-spacing: 0;
    display: flex;
    justify-content: space-between;
    align-items: baseline;
  }
  .tune-label em {
    font-style: normal;
    font-family: ui-monospace, monospace;
    font-size: 12px;
    color: var(--ember);
  }
  .tune-row input[type="range"] {
    grid-column: 1 / -1;
    width: 100%;
    accent-color: var(--ember);
  }
  .tune-hint {
    grid-column: 1 / -1;
    font-size: 11.5px;
    color: var(--ink-faint);
  }
</style>
