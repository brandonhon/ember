<script lang="ts">
  import {
    user,
    feverAPIKey,
    appVersion,
    theme,
    density,
    refreshMe,
    showSummary,
    THEMES,
    customPalette,
    branding,
    refreshBranding,
  } from "../lib/stores";
  import { api, ApiError, type StarterPack, type StarterImportResult, type LLMStatus, type DBStatus, type UserStats, type UserDigest, type PasskeySummary } from "../lib/api";
  import type { PushSubscriptionSummary, EmailInbox } from "../lib/types";
  import { createPasskey, passkeySupported } from "../lib/passkey";
  import { enablePush, pushSupported } from "../lib/push";
  import { onMount } from "svelte";
  import { refreshSidebar, loadArticles, activeView } from "../lib/stores";
  import FilterManager from "./FilterManager.svelte";
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

  type Section = "profile" | "inbox" | "passkeys" | "notifications" | "preferences" | "stats" | "digest" | "mobile" | "filters" | "feeds" | "users" | "starter" | "import" | "llm" | "branding" | "database" | "session" | "email" | "about";
  let section = $state<Section>("profile");

  // Mobile detection (matches App.svelte's 900px breakpoint). On mobile the
  // settings modal switches to a drill-down: section list → section detail.
  // sectionPicked tracks whether the user has tapped into a section yet.
  let isMobile = $state(
    typeof window !== "undefined" && window.matchMedia?.("(max-width: 900px)").matches
  );
  let sectionPicked = $state(false);
  $effect(() => {
    if (typeof window === "undefined") return;
    const m = window.matchMedia("(max-width: 900px)");
    const handler = (e: MediaQueryListEvent) => {
      isMobile = e.matches;
      if (!e.matches) sectionPicked = false;
    };
    m.addEventListener("change", handler);
    return () => m.removeEventListener("change", handler);
  });
  function pickSection(s: Section) {
    section = s;
    if (isMobile) sectionPicked = true;
  }
  function mobileBackToList() { sectionPicked = false; }

  // Per-section glyphs shown only in the mobile settings list (the .nav-ic
  // span is display:none on desktop, so the desktop rail stays text-only).
  const NAV_ICONS: Record<string, string> = {
    profile: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></svg>`,
    passkeys: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="10" rx="2"/><path d="M7 11V8a5 5 0 0 1 10 0v3"/></svg>`,
    notifications: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M10.3 21a2 2 0 0 0 3.4 0"/></svg>`,
    inbox: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 7 9 6 9-6"/></svg>`,
    preferences: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M3 12h3M18 12h3M12 3v3M12 18v3"/></svg>`,
    filters: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 5h18l-7 8v6l-4-2v-4z"/></svg>`,
    feeds: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 11a9 9 0 0 1 9 9M4 4a16 16 0 0 1 16 16"/><circle cx="5" cy="19" r="1.5" fill="currentColor" stroke="none"/></svg>`,
    digest: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 19V5m6 14v-9m6 9V8m4 11H2"/></svg>`,
    stats: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 3v18h18M7 15l4-5 3 3 5-7"/></svg>`,
    mobile: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="7" y="3" width="10" height="18" rx="2"/></svg>`,
    import: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="M7 10l5 5 5-5M12 15V3"/></svg>`,
    starter: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2l2.4 5 5.6.8-4 4 1 5.6L12 20l-5 2.4 1-5.6-4-4 5.6-.8z"/></svg>`,
    llm: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M9 9h6v6H9z"/></svg>`,
    branding: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 3l8 4-8 4-8-4z"/><path d="M4 11l8 4 8-4"/></svg>`,
    database: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5"/></svg>`,
    session: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>`,
    email: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 7 9 6 9-6"/></svg>`,
    users: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="9" cy="8" r="3"/><path d="M3 21a6 6 0 0 1 12 0M16 6a3 3 0 0 1 0 6"/></svg>`,
    about: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/></svg>`,
  };

  // --- Import & Data section -------------------------------------------
  let importTab = $state<"live" | "file">("live");
  let ttUrl = $state("");
  let ttUser = $state("");
  let ttPass = $state("");
  let ttFeeds = $state(true);
  let ttStarred = $state(true);
  let ttArchived = $state(true);
  let importBusy = $state(false);
  let importMsg = $state("");
  let importErr = $state("");
  let ttrssFileInput: HTMLInputElement | undefined = $state();
  let opmlFileInput: HTMLInputElement | undefined = $state();

  async function ttrssLivePull() {
    if (!ttUrl.trim() || !ttUser.trim()) {
      importErr = "URL and username are required";
      return;
    }
    if (!ttFeeds && !ttStarred && !ttArchived) {
      importErr = "Pick at least one of Subscriptions / Starred / Archived";
      return;
    }
    importErr = "";
    importMsg = "";
    importBusy = true;
    try {
      const res = await api.importTTRSSAPI({
        url: ttUrl.trim(),
        username: ttUser.trim(),
        password: ttPass,
        import_feeds: ttFeeds,
        import_starred: ttStarred,
        import_archived: ttArchived,
      });
      const parts: string[] = [];
      if (ttFeeds) {
        let s = `${res.data.feeds} new subscriptions`;
        if (res.data.feeds_existing > 0)
          s += ` (${res.data.feeds_existing} already subscribed, skipped)`;
        parts.push(s);
      }
      if (ttStarred || ttArchived)
        parts.push(`${res.data.imported} of ${res.data.total} articles`);
      importMsg = `Migrated ${parts.join(" and ")}.`;
      await refreshSidebar();
    } catch (e) {
      importErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      importBusy = false;
    }
  }

  async function ttrssFilePick(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importErr = "";
    importMsg = "Importing…";
    importBusy = true;
    try {
      const res = await api.importTTRSS(file);
      importMsg = `Imported ${res.data.imported} of ${res.data.total} articles.`;
      await refreshSidebar();
    } catch (err) {
      importErr = err instanceof ApiError ? err.message : String(err);
      importMsg = "";
    } finally {
      input.value = "";
      importBusy = false;
    }
  }

  async function opmlFilePick(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importErr = "";
    importMsg = "Importing…";
    importBusy = true;
    try {
      const res = await api.importOPML(file);
      importMsg = `Imported ${res.data.imported} subscriptions.`;
      await refreshSidebar();
    } catch (err) {
      importErr = err instanceof ApiError ? err.message : String(err);
      importMsg = "";
    } finally {
      input.value = "";
      importBusy = false;
    }
  }

  async function exportOPML() {
    try {
      const res = await api.exportOPML();
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "ember.opml";
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      importErr = err instanceof ApiError ? err.message : String(err);
    }
  }

  // Human-readable label for the active section, used in the mobile drill-down
  // header. Keeps in sync with the nav button text.
  const sectionLabel = $derived.by((): string => {
    switch (section) {
      case "profile": return "Profile";
      case "passkeys": return "Passkeys";
      case "notifications": return "Notifications";
      case "inbox": return "Email inbox";
      case "preferences": return "Preferences";
      case "mobile": return "Mobile clients";
      case "filters": return "Filters";
      case "feeds": return "Feeds";
      case "stats": return "Reading stats";
      case "digest": return "Daily digest";
      case "starter": return "Starter packs";
      case "import": return "Import & migrate";
      case "llm": return "Language model";
      case "branding": return "Branding";
      case "database": return "Database";
      case "session": return "Sessions";
      case "email": return "Email / SMTP";
      case "users": return "Users";
      case "about": return "About";
    }
  });

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

  // A pack is "installed" when the user is subscribed to every feed in it;
  // the button label then flips from Add to Remove.
  function packInstalled(p: StarterPack): boolean {
    return p.subscribed >= p.feed_urls.length;
  }

  async function loadStarterPacks() {
    // Always refresh so subscribed counts stay in sync after add/remove.
    try {
      const res = await api.listStarterPacks();
      starterPacks = res.data ?? [];
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
      // Starter-pack ingest runs in detached goroutines; the first refresh
      // sees the new feeds but not yet the articles/summary-queue. Pull the
      // current view immediately and re-poll counts a bit later.
      await loadArticles($activeView);
      setTimeout(() => { void refreshSidebar(); }, 2000);
      await loadStarterPacks();
    } catch (e) {
      starterErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      starterBusy = "";
      setTimeout(() => (starterMsg = ""), 4000);
    }
  }

  async function removeStarter(slug: string) {
    if (!confirm("Unsubscribe from every feed in this pack? Feeds you have starred articles in are not deleted.")) {
      return;
    }
    starterBusy = slug;
    starterMsg = "";
    starterErr = "";
    try {
      const res = await api.removeStarterPack(slug);
      const r = res.data;
      const parts: string[] = [];
      if (r.feeds_removed) parts.push(`${r.feeds_removed} removed`);
      if (r.not_subscribed) parts.push(`${r.not_subscribed} not subscribed`);
      if (r.category_removed) parts.push("folder cleared");
      starterMsg = parts.join(" · ") || "Nothing to remove";
      await refreshSidebar();
      await loadStarterPacks();
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

  // Email inbox state ------------------------------------------------
  let inbox = $state<EmailInbox | null>(null);
  let inboxErr = $state("");
  let inboxMsg = $state("");
  let inboxBusy = $state(false);
  async function loadInbox() {
    inboxErr = "";
    try {
      const res = await api.getInbox();
      inbox = res.data ?? null;
    } catch (e) {
      inboxErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function copyInboxAddress() {
    if (!inbox?.address) return;
    try {
      await navigator.clipboard.writeText(inbox.address);
      inboxMsg = "Address copied to clipboard.";
      setTimeout(() => (inboxMsg = ""), 2500);
    } catch (e) {
      inboxErr = e instanceof Error ? e.message : String(e);
    }
  }
  async function onRotateInbox() {
    inboxErr = "";
    inboxBusy = true;
    try {
      const res = await api.rotateInbox();
      inbox = res.data ?? inbox;
      inboxMsg = "New address generated. Old address still works for 7 days.";
      setTimeout(() => (inboxMsg = ""), 4000);
    } catch (e) {
      inboxErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      inboxBusy = false;
    }
  }

  // Push notifications state ----------------------------------------
  const canPush = pushSupported();
  let pushSubs = $state<PushSubscriptionSummary[]>([]);
  let pushErr = $state("");
  let pushMsg = $state("");
  let pushBusy = $state(false);
  async function loadPushSubs() {
    pushErr = "";
    try {
      const res = await api.pushSubscriptions();
      pushSubs = res.data ?? [];
    } catch (e) {
      pushErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function onEnablePush() {
    pushErr = "";
    pushMsg = "";
    pushBusy = true;
    try {
      await enablePush();
      pushMsg = "Notifications enabled on this device.";
      await loadPushSubs();
    } catch (e) {
      pushErr = e instanceof Error ? e.message : String(e);
    } finally {
      pushBusy = false;
      setTimeout(() => (pushMsg = ""), 3500);
    }
  }
  async function onDeletePushSub(id: number) {
    try {
      await api.pushUnsubscribe(id);
      await loadPushSubs();
    } catch (e) {
      pushErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function onSendTestPush() {
    pushErr = "";
    pushMsg = "";
    try {
      const res = await api.pushTest();
      pushMsg = `Sent to ${res.data?.sent ?? 0} device(s).`;
      if ((res.data?.removed ?? 0) > 0) {
        // Refresh list — server pruned dead subs.
        await loadPushSubs();
      }
    } catch (e) {
      pushErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      setTimeout(() => (pushMsg = ""), 3500);
    }
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
    if (section === "session" && $user?.is_admin) void loadSessionTTL();
    if (section === "email" && $user?.is_admin) void loadEmailSettings();
    if (section === "feeds" && $user?.is_admin) void loadFeedSettings();
    if (section === "users" && $user?.is_admin) void loadUsers();
    if (section === "stats") void loadStats();
    if (section === "digest") void loadDigest();
    if (section === "passkeys") void loadPasskeys();
    if (section === "notifications") void loadPushSubs();
    if (section === "inbox") void loadInbox();
  });

  // Admin session TTL ------------------------------------------------
  let sessionTTL = $state<{ ttl_seconds: number; source: "admin" | "default" } | null>(null);
  let sessionTTLDraft = $state(86400); // 24h default
  let sessionMsg = $state("");
  let sessionErr = $state("");
  let sessionBusy = $state(false);
  async function loadSessionTTL() {
    sessionErr = "";
    try {
      const res = await api.getSessionTTL();
      sessionTTL = res.data;
      sessionTTLDraft = res.data.ttl_seconds;
    } catch (e) {
      sessionErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function saveSessionTTL() {
    sessionBusy = true;
    sessionMsg = "";
    sessionErr = "";
    try {
      const res = await api.setSessionTTL(sessionTTLDraft);
      sessionTTL = res.data;
      sessionMsg = "Saved";
    } catch (e) {
      sessionErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      sessionBusy = false;
      setTimeout(() => (sessionMsg = ""), 3000);
    }
  }
  // Presets cover the common cases; admins who want odd values can edit
  // the number input directly. Lower bound matches the server's
  // minSessionTTL (5min); upper bound matches maxSessionTTL (90d).
  const ttlPresets: { label: string; seconds: number }[] = [
    { label: "1 hour", seconds: 3600 },
    { label: "4 hours", seconds: 14400 },
    { label: "24 hours (default)", seconds: 86400 },
    { label: "7 days", seconds: 604800 },
    { label: "30 days", seconds: 2592000 },
  ];

  onMount(() => {
    // Allow opening Settings directly to the starter pack section via hash.
    if (typeof location !== "undefined" && location.hash === "#starter") {
      section = "starter";
    }
  });

  // Admin: Email / SMTP --------------------------------------------------
  // The SMTP password is write-only: GET returns whether one is stored, never
  // the value. To keep it: leave the field blank on save. To change: type the
  // new password. To remove: tick "Clear stored password" and save.
  type EmailDraft = {
    host: string;
    port: number;
    username: string;
    password: string;
    clear_password: boolean;
    from: string;
    starttls: boolean;
    initial_backlog_hours: number;
  };
  let emailDraft = $state<EmailDraft>({
    host: "", port: 587, username: "", password: "", clear_password: false,
    from: "", starttls: true, initial_backlog_hours: 48,
  });
  // Preset choices for the feed-check interval (all within the 5m–24h bounds).
  const pollIntervalPresets = [
    { label: "5 minutes", seconds: 300 },
    { label: "15 minutes", seconds: 900 },
    { label: "30 minutes", seconds: 1800 },
    { label: "1 hour", seconds: 3600 },
    { label: "2 hours", seconds: 7200 },
    { label: "6 hours", seconds: 21600 },
    { label: "12 hours", seconds: 43200 },
    { label: "24 hours", seconds: 86400 },
  ];

  // --- Feeds section (admin: poll interval) ----------------------------
  let feedSettings = $state({
    poll_min_interval_seconds: 1800,
    reading_window_hours: 24,
    search_window_hours: 48,
    window_hours_floor: 24,
    window_hours_ceil: 168,
  });
  let feedBusy = $state(false);
  let feedMsg = $state("");
  let feedErr = $state("");
  async function loadFeedSettings() {
    feedErr = "";
    try {
      const res = await api.getAdminSettings();
      feedSettings.poll_min_interval_seconds = res.data.poll_min_interval_seconds;
      feedSettings.reading_window_hours = res.data.reading_window_hours;
      feedSettings.search_window_hours = res.data.search_window_hours;
      feedSettings.window_hours_floor = res.data.window_hours_floor;
      feedSettings.window_hours_ceil = res.data.window_hours_ceil;
    } catch (e) {
      feedErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function saveFeedSettings() {
    feedBusy = true;
    feedMsg = "";
    feedErr = "";
    try {
      const res = await api.setAdminSettings({
        poll_min_interval_seconds: Number(feedSettings.poll_min_interval_seconds) || 1800,
        reading_window_hours: Number(feedSettings.reading_window_hours) || 24,
        search_window_hours: Number(feedSettings.search_window_hours) || 48,
      });
      feedSettings.poll_min_interval_seconds = res.data.poll_min_interval_seconds;
      feedSettings.reading_window_hours = res.data.reading_window_hours;
      feedSettings.search_window_hours = res.data.search_window_hours;
      feedMsg = "Saved";
    } catch (e) {
      feedErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      feedBusy = false;
    }
  }
  let emailLoaded = $state<import("../lib/api").AdminSettings | null>(null);
  let emailBusy = $state(false);
  let emailMsg = $state("");
  let emailErr = $state("");
  let testRecipient = $state("");
  let testBusy = $state(false);

  async function loadEmailSettings() {
    emailErr = "";
    try {
      const res = await api.getAdminSettings();
      emailLoaded = res.data;
      emailDraft = {
        host: res.data.smtp.host,
        port: res.data.smtp.port,
        username: res.data.smtp.username,
        password: "",
        clear_password: false,
        from: res.data.smtp.from,
        starttls: res.data.smtp.starttls,
        initial_backlog_hours: res.data.initial_backlog_hours,
      };
    } catch (e) {
      emailErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function saveEmailSettings() {
    emailBusy = true;
    emailMsg = "";
    emailErr = "";
    try {
      const patch: import("../lib/api").AdminSettingsPatch = {
        smtp: {
          host: emailDraft.host.trim(),
          port: Number(emailDraft.port) || 0,
          username: emailDraft.username.trim(),
          from: emailDraft.from.trim(),
          starttls: !!emailDraft.starttls,
        },
        initial_backlog_hours: Math.max(0, Number(emailDraft.initial_backlog_hours) || 0),
      };
      // Only send the password when the admin actually typed one, OR when
      // they're explicitly clearing the stored value. Otherwise the server
      // leaves the existing password alone.
      if (emailDraft.clear_password) {
        patch.smtp!.clear_password = true;
      } else if (emailDraft.password) {
        patch.smtp!.password = emailDraft.password;
      }
      const res = await api.setAdminSettings(patch);
      emailLoaded = res.data;
      emailDraft.password = "";
      emailDraft.clear_password = false;
      emailMsg = "Saved";
    } catch (e) {
      emailErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      emailBusy = false;
      setTimeout(() => (emailMsg = ""), 3000);
    }
  }
  async function sendTestEmail() {
    testBusy = true;
    emailMsg = "";
    emailErr = "";
    try {
      const res = await api.testEmail(testRecipient.trim() || undefined);
      emailMsg = `Test sent to ${res.data.sent_to}`;
    } catch (e) {
      emailErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      testBusy = false;
      setTimeout(() => (emailMsg = ""), 4000);
    }
  }

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

  // Toggling stays a child modal so we don't double-render filters.
  let showFilters = $state(false);

  // Users admin: full inline section (no modal). Mirrors the structure of
  // Database / Sessions / LLM admin sections.
  type UserRow = {
    id: number;
    username: string;
    email: string;
    is_admin: boolean;
    created_at: number;
  };
  let usersList = $state<UserRow[]>([]);
  let usersErr = $state("");
  let usersMsg = $state("");
  let usersBusy = $state<string>(""); // active row action key, e.g. "admin:5" or "delete:5"
  let newUsername = $state("");
  let newUserEmail = $state("");
  let newUserPassword = $state("");
  let newUserIsAdmin = $state(false);

  async function loadUsers() {
    usersErr = "";
    try {
      const res = await api.listUsers();
      usersList = (res.data ?? []) as UserRow[];
    } catch (e) {
      usersErr = e instanceof ApiError ? e.message : String(e);
    }
  }
  async function createNewUser() {
    if (!newUsername.trim() || !newUserPassword.trim()) {
      usersErr = "username and password required";
      return;
    }
    usersBusy = "create";
    usersErr = "";
    usersMsg = "";
    try {
      await api.createUser({
        username: newUsername.trim(),
        email: newUserEmail.trim() || undefined,
        password: newUserPassword,
        is_admin: newUserIsAdmin,
      });
      newUsername = "";
      newUserEmail = "";
      newUserPassword = "";
      newUserIsAdmin = false;
      await loadUsers();
      usersMsg = "User created";
    } catch (e) {
      usersErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      usersBusy = "";
      setTimeout(() => (usersMsg = ""), 3000);
    }
  }
  async function toggleAdmin(u: UserRow) {
    const next = !u.is_admin;
    usersBusy = `admin:${u.id}`;
    usersErr = "";
    try {
      await api.updateUser(u.id, { is_admin: next });
      // Optimistic local update — refresh would also fix it but this
      // makes the toggle feel instant.
      usersList = usersList.map((x) => (x.id === u.id ? { ...x, is_admin: next } : x));
    } catch (e) {
      usersErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      usersBusy = "";
    }
  }
  function askDeleteUser(u: UserRow) {
    confirmReq = {
      title: "Delete user?",
      message: `Permanently delete the account "${u.username}". This cannot be undone.`,
      confirmLabel: "Delete",
      destructive: true,
      run: () => deleteUserById(u.id),
    };
  }
  async function deleteUserById(id: number) {
    usersBusy = `delete:${id}`;
    usersErr = "";
    try {
      await api.deleteUser(id);
      await loadUsers();
    } catch (e) {
      usersErr = e instanceof ApiError ? e.message : String(e);
    } finally {
      usersBusy = "";
    }
  }

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
  <div class="modal" class:mobile={isMobile} on:click|stopPropagation>
    <header>
      {#if isMobile && sectionPicked}
        <button class="back-btn" on:click={mobileBackToList} aria-label="Back to settings menu" data-testid="settings-back">
          <svg viewBox="0 0 24 24" width="22" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M15 18l-6-6 6-6"/></svg>
        </button>
        <h2 id="settings-title">{sectionLabel}</h2>
      {:else}
        <h2 id="settings-title">Settings</h2>
      {/if}
      <button class="close" on:click={onCloseAll} aria-label="Close">×</button>
    </header>

    <div class="layout" data-view={isMobile ? (sectionPicked ? "detail" : "list") : "split"}>
      <nav class="nav" aria-label="Settings sections">
        <div class="nav-group">
          <div class="nav-label">Account</div>
          <button class:active={section === "profile"} on:click={() => pickSection("profile")} data-testid="settings-profile"><span class="nav-ic">{@html NAV_ICONS.profile}</span>Profile</button>
          <button class:active={section === "passkeys"} on:click={() => pickSection("passkeys")} data-testid="settings-passkeys"><span class="nav-ic">{@html NAV_ICONS.passkeys}</span>Passkeys</button>
          <button class:active={section === "notifications"} on:click={() => pickSection("notifications")} data-testid="settings-notifications"><span class="nav-ic">{@html NAV_ICONS.notifications}</span>Notifications</button>
          <button class:active={section === "inbox"} on:click={() => pickSection("inbox")} data-testid="settings-inbox"><span class="nav-ic">{@html NAV_ICONS.inbox}</span>Email inbox</button>
        </div>
        <div class="nav-group">
          <div class="nav-label">Reading</div>
          <button class:active={section === "preferences"} on:click={() => pickSection("preferences")} data-testid="settings-preferences"><span class="nav-ic">{@html NAV_ICONS.preferences}</span>Preferences</button>
          <button class:active={section === "filters"} on:click={() => pickSection("filters")} data-testid="settings-filters"><span class="nav-ic">{@html NAV_ICONS.filters}</span>Filters</button>
          {#if $user?.is_admin}
            <button class:active={section === "feeds"} on:click={() => pickSection("feeds")} data-testid="settings-feeds"><span class="nav-ic nav-ic-admin">{@html NAV_ICONS.feeds}</span>Feeds</button>
          {/if}
          <button class:active={section === "digest"} on:click={() => pickSection("digest")} data-testid="settings-digest"><span class="nav-ic">{@html NAV_ICONS.digest}</span>Daily digest</button>
          <button class:active={section === "stats"} on:click={() => pickSection("stats")} data-testid="settings-stats"><span class="nav-ic">{@html NAV_ICONS.stats}</span>Reading stats</button>
          <button class:active={section === "mobile"} on:click={() => pickSection("mobile")} data-testid="settings-mobile"><span class="nav-ic">{@html NAV_ICONS.mobile}</span>Mobile clients</button>
        </div>
        <div class="nav-group">
          <div class="nav-label">Import &amp; Data</div>
          <button class:active={section === "import"} on:click={() => pickSection("import")} data-testid="settings-import"><span class="nav-ic">{@html NAV_ICONS.import}</span>Import &amp; migrate</button>
          <button class:active={section === "starter"} on:click={() => pickSection("starter")} data-testid="settings-starter"><span class="nav-ic">{@html NAV_ICONS.starter}</span>Starter packs</button>
        </div>

        {#if $user?.is_admin}
          <div class="nav-group">
            <div class="nav-label">Administration</div>
            <button class:active={section === "llm"} on:click={() => pickSection("llm")} data-testid="settings-llm"><span class="nav-ic nav-ic-admin">{@html NAV_ICONS.llm}</span>Language model</button>
            <button class:active={section === "branding"} on:click={() => pickSection("branding")} data-testid="settings-branding"><span class="nav-ic nav-ic-admin">{@html NAV_ICONS.branding}</span>Branding</button>
            <button class:active={section === "database"} on:click={() => pickSection("database")} data-testid="settings-database"><span class="nav-ic nav-ic-admin">{@html NAV_ICONS.database}</span>Database</button>
            <button class:active={section === "session"} on:click={() => pickSection("session")} data-testid="settings-session"><span class="nav-ic nav-ic-admin">{@html NAV_ICONS.session}</span>Sessions</button>
            <button class:active={section === "email"} on:click={() => pickSection("email")} data-testid="settings-email"><span class="nav-ic nav-ic-admin">{@html NAV_ICONS.email}</span>Email / SMTP</button>
            <button class:active={section === "users"} on:click={() => pickSection("users")} data-testid="settings-users"><span class="nav-ic nav-ic-admin">{@html NAV_ICONS.users}</span>Users</button>
          </div>
        {/if}
        <div class="nav-group">
          <div class="nav-label">System</div>
          <button class:active={section === "about"} on:click={() => pickSection("about")} data-testid="settings-about"><span class="nav-ic">{@html NAV_ICONS.about}</span>About</button>
        </div>
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

        {#if section === "inbox"}
          <h3>Email inbox</h3>
          <p class="hint">
            Each user gets a unique address. Mail sent to it lands in a
            "Newsletters" feed — useful for Substack, Beehiiv, or any
            newsletter that doesn't expose an RSS feed.
          </p>
          {#if inbox && !inbox.enabled}
            <div class="hint">
              The administrator hasn't configured an email domain
              (<code>EMBER_EMAIL_DOMAIN</code>). See
              <a href="/docs/email-inbox" target="_blank" rel="noopener noreferrer">the setup docs</a>.
            </div>
          {:else if inbox && inbox.address}
            <div class="kv" style="grid-template-columns: 1fr auto;">
              <dt>Your address</dt>
              <dd>
                <code data-testid="inbox-address">{inbox.address}</code>
              </dd>
            </div>
            <div class="actions" style="margin-top: 12px;">
              <button on:click={copyInboxAddress} data-testid="inbox-copy">Copy address</button>
              <button class="ghost" on:click={onRotateInbox} disabled={inboxBusy} data-testid="inbox-rotate">
                {inboxBusy ? "Rotating…" : "Rotate address"}
              </button>
            </div>
            {#if inboxErr}<p class="error" data-testid="inbox-err">{inboxErr}</p>{/if}
            {#if inboxMsg}<p class="ok" data-testid="inbox-msg">{inboxMsg}</p>{/if}
            <p class="hint" style="margin-top: 14px;">
              Sign up for a newsletter using this address. New issues
              show up in the Newsletters feed within seconds of arrival.
              Rotate the address if it gets sold or leaked — the old
              address keeps working for 7 days.
            </p>
          {:else}
            <p class="hint">Loading…</p>
          {/if}
        {/if}

        {#if section === "notifications"}
          <h3>Notifications</h3>
          <p class="hint">
            Web Push delivers reminders to your browser or installed PWA
            even when Ember isn't open. Each device you enable shows up
            below; revoke any you don't recognize.
          </p>
          {#if !canPush}
            <div class="error" data-testid="push-unsupported">
              This browser doesn't support Web Push notifications.
            </div>
          {:else}
            <div class="actions" style="margin-bottom: 12px;">
              <button on:click={onEnablePush} disabled={pushBusy} data-testid="push-enable">
                {pushBusy ? "Enabling…" : "Enable on this device"}
              </button>
              <button class="ghost" on:click={onSendTestPush} disabled={pushSubs.length === 0} data-testid="push-test">
                Send test notification
              </button>
            </div>
            {#if pushErr}<p class="error" data-testid="push-err">{pushErr}</p>{/if}
            {#if pushMsg}<p class="ok" data-testid="push-msg">{pushMsg}</p>{/if}
            <h4>Registered devices</h4>
            {#if pushSubs.length === 0}
              <p class="hint">No devices registered yet.</p>
            {:else}
              <ul class="list">
                {#each pushSubs as s (s.id)}
                  <li class="list-row">
                    <div>
                      <div class="list-title">{s.user_agent || "Unknown browser"}</div>
                    </div>
                    <button class="btn-danger" on:click={() => onDeletePushSub(s.id)} aria-label="Revoke device">
                      Revoke
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
                <div class="pref-hint">Pick four colors — the rest of the palette is derived automatically.</div>
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
                <label>
                  <span>Links</span>
                  <input type="color" bind:value={$customPalette.link} data-testid="custom-link" />
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
              <div class="pref-label">AI summary card</div>
              <div class="pref-hint">When off, the article body is shown directly with no summary card.</div>
            </div>
            <div class="seg">
              <button class:on={$showSummary} on:click={() => showSummary.set(true)} data-testid="pref-summary-on">On</button>
              <button class:on={!$showSummary} on:click={() => showSummary.set(false)} data-testid="pref-summary-off">Off</button>
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

        {#if section === "import"}
          <h3>Import &amp; migrate</h3>
          <p class="hint">Bring your library and subscriptions into Ember. Nothing here touches your existing feeds.</p>
          {#if importErr}<p class="error" data-testid="import-error">{importErr}</p>{/if}
          {#if importMsg}<p class="ok" data-testid="import-msg">{importMsg}</p>{/if}

          <input type="file" accept=".xml,application/xml,text/xml" bind:this={ttrssFileInput} on:change={ttrssFilePick} style="display:none" data-testid="ttrss-file-input" />
          <input type="file" accept=".opml,.xml,application/xml,text/xml" bind:this={opmlFileInput} on:change={opmlFilePick} style="display:none" data-testid="opml-file-input" />

          <div class="import-card">
            <h4>Tiny Tiny RSS</h4>
            <p class="import-sub">Migrate your subscriptions and starred &amp; archived articles from a running instance, or upload an article export file.</p>
            <div class="import-seg" role="tablist">
              <button role="tab" class:on={importTab === "live"} on:click={() => (importTab = "live")} data-testid="ttrss-tab-live">Migrate from running TT-RSS</button>
              <button role="tab" class:on={importTab === "file"} on:click={() => (importTab = "file")} data-testid="ttrss-tab-file">Upload export file</button>
            </div>
            {#if importTab === "live"}
              <label><span>TT-RSS URL</span>
                <input type="text" bind:value={ttUrl} placeholder="rss.example.com/tt-rss" disabled={importBusy} data-testid="ttrss-url" />
              </label>
              <label><span>Username</span>
                <input type="text" bind:value={ttUser} disabled={importBusy} data-testid="ttrss-user" />
              </label>
              <label><span>Password</span>
                <input type="password" bind:value={ttPass} disabled={importBusy} data-testid="ttrss-pass" />
              </label>
              <div class="import-checks">
                <label class="inline"><input type="checkbox" bind:checked={ttFeeds} disabled={importBusy} data-testid="ttrss-feeds" /> Subscriptions &amp; folders</label>
                <label class="inline"><input type="checkbox" bind:checked={ttStarred} disabled={importBusy} /> Starred</label>
                <label class="inline"><input type="checkbox" bind:checked={ttArchived} disabled={importBusy} /> Archived</label>
              </div>
              <p class="import-note">Feeds you’re already subscribed to are skipped, so it’s safe to run more than once. Enable “API access” in your TT-RSS Preferences first. If TT-RSS lives under a subpath (e.g. <code>/tt-rss</code>), include it — we append <code>/api/</code>. Credentials are used only for this import and never stored.</p>
              <div class="actions">
                <button on:click={ttrssLivePull} disabled={importBusy} data-testid="ttrss-start">{importBusy ? "Importing…" : "Start migration"}</button>
              </div>
            {:else}
              <p class="import-note">Export your Starred &amp; Archived articles from TT-RSS (the import/export plugin produces an <code>.xml</code> file), then upload it here. <strong>Subscriptions aren’t included in this file</strong> — to bring over your feed list, use “Migrate from running TT-RSS” above, or import an OPML export below.</p>
              <div class="actions">
                <button on:click={() => ttrssFileInput?.click()} disabled={importBusy} data-testid="ttrss-file-pick">Choose .xml file…</button>
              </div>
            {/if}
          </div>

          <div class="import-card">
            <h4>OPML subscriptions</h4>
            <p class="import-sub">Import or export your feed list in the universal OPML format.</p>
            <div class="actions" style="justify-content:flex-start">
              <button on:click={() => opmlFileInput?.click()} disabled={importBusy} data-testid="open-opml-import">Import OPML…</button>
              <button class="ghost" on:click={exportOPML} data-testid="export-opml">Export OPML</button>
            </div>
          </div>
        {/if}

        {#if section === "starter"}
          <h3>Starter packs</h3>
          <p class="hint">Curated bundles of feeds. Click a pack to create the folder and subscribe — already-subscribed feeds are skipped.</p>
          {#if starterErr}<p class="error">{starterErr}</p>{/if}
          {#if starterMsg}<p class="ok" data-testid="starter-msg">{starterMsg}</p>{/if}
          <div class="pack-list">
            {#each starterPacks as p (p.slug)}
              {@const installed = packInstalled(p)}
              <div class="pack">
                <div>
                  <div class="pack-name">
                    <span class="pack-dot" style="background:{p.color}"></span>
                    {p.name}
                  </div>
                  <div class="pack-hint">
                    {p.feed_urls.length} feeds{#if p.subscribed > 0 && !installed} · {p.subscribed} subscribed{/if}
                  </div>
                </div>
                {#if installed}
                  <button
                    on:click={() => removeStarter(p.slug)}
                    disabled={starterBusy === p.slug}
                    data-testid="starter-remove-{p.slug}"
                    class="pack-btn pack-btn-remove"
                  >
                    {starterBusy === p.slug ? "Removing…" : "Remove pack"}
                  </button>
                {:else}
                  <button
                    on:click={() => importStarter(p.slug)}
                    disabled={starterBusy === p.slug}
                    data-testid="starter-import-{p.slug}"
                    class="pack-btn"
                  >
                    {starterBusy === p.slug ? "Adding…" : "Add pack"}
                  </button>
                {/if}
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
            <input type="text" bind:value={brandingDraft.page_title} placeholder="Ember" data-testid="branding-title" />
          </label>
          <label>
            <span>Favicon URL</span>
            <input type="text" bind:value={brandingDraft.favicon_url} placeholder="/icon.svg or data:image/svg+xml;..." data-testid="branding-favicon" />
            <span class="pref-hint">Public URL (e.g. /icon.svg, https://…/icon.png) or a data: URI. Hard-refresh after changing.</span>
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
          <p class="hint">Admin-only. Create new accounts, toggle admin, and remove users.</p>
          {#if usersErr}<p class="error" data-testid="users-error">{usersErr}</p>{/if}
          {#if usersMsg}<p class="ok" data-testid="users-msg">{usersMsg}</p>{/if}

          <h4>New user</h4>
          <div class="row">
            <label>
              <span>Username</span>
              <input
                type="text"
                bind:value={newUsername}
                autocomplete="username"
                data-testid="new-user-username"
              />
            </label>
            <label>
              <span>Email (optional)</span>
              <input
                type="email"
                bind:value={newUserEmail}
                autocomplete="email"
                data-testid="new-user-email"
              />
            </label>
          </div>
          <div class="row">
            <label>
              <span>Password</span>
              <input
                type="password"
                bind:value={newUserPassword}
                autocomplete="new-password"
                data-testid="new-user-password"
              />
            </label>
          </div>
          <div class="pref-row">
            <div>
              <div class="pref-label">Admin</div>
              <div class="pref-hint">Grants access to Settings → Branding / Database / Sessions / Users / LLM.</div>
            </div>
            <div class="seg">
              <button class:on={newUserIsAdmin} on:click={() => (newUserIsAdmin = true)} data-testid="new-user-admin-on">Yes</button>
              <button class:on={!newUserIsAdmin} on:click={() => (newUserIsAdmin = false)} data-testid="new-user-admin-off">No</button>
            </div>
          </div>
          <div class="actions">
            <button
              on:click={createNewUser}
              disabled={usersBusy === "create"}
              data-testid="create-user-submit"
            >
              {usersBusy === "create" ? "Creating…" : "Create user"}
            </button>
          </div>

          <h4>Existing users</h4>
          {#if usersList.length === 0}
            <p class="muted">Loading…</p>
          {:else}
            <table class="llm-table" data-testid="users-table">
              <thead>
                <tr>
                  <th>Username</th>
                  <th>Email</th>
                  <th>Admin</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {#each usersList as u (u.id)}
                  <tr data-testid="user-row-{u.id}">
                    <td>
                      <strong>{u.username}</strong>
                      {#if $user?.id === u.id}<span class="muted"> (you)</span>{/if}
                    </td>
                    <td>{u.email || "—"}</td>
                    <td>
                      <div class="seg">
                        <button
                          class:on={u.is_admin}
                          on:click={() => toggleAdmin(u)}
                          disabled={usersBusy === `admin:${u.id}` || $user?.id === u.id}
                          data-testid="user-admin-yes-{u.id}"
                          title={$user?.id === u.id ? "Cannot change your own admin status" : ""}
                        >Yes</button>
                        <button
                          class:on={!u.is_admin}
                          on:click={() => toggleAdmin(u)}
                          disabled={usersBusy === `admin:${u.id}` || $user?.id === u.id}
                          data-testid="user-admin-no-{u.id}"
                          title={$user?.id === u.id ? "Cannot change your own admin status" : ""}
                        >No</button>
                      </div>
                    </td>
                    <td class="llm-actions">
                      {#if $user?.id !== u.id}
                        <button
                          class="ghost-btn"
                          on:click={() => askDeleteUser(u)}
                          disabled={usersBusy === `delete:${u.id}`}
                          data-testid="user-delete-{u.id}"
                        >
                          {usersBusy === `delete:${u.id}` ? "Deleting…" : "Delete"}
                        </button>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        {/if}

        {#if section === "session" && $user?.is_admin}
          <h3>Sessions</h3>
          <p class="hint">
            Server-wide session lifetime — how long a freshly-issued login cookie stays valid.
            Affects newly-issued sessions only; existing sessions keep their stored expiry.
            Range: 5 minutes to 90 days.
          </p>
          {#if sessionErr}<p class="error" data-testid="session-error">{sessionErr}</p>{/if}
          {#if sessionMsg}<p class="ok" data-testid="session-msg">{sessionMsg}</p>{/if}
          {#if sessionTTL}
            <p class="pref-hint">
              Active TTL: {Math.round(sessionTTL.ttl_seconds / 3600)}h
              <span style="opacity:0.6">({sessionTTL.source === "admin" ? "set in admin UI" : "default / env var"})</span>
            </p>
          {/if}
          <label class="pref-row">
            <span>Preset</span>
            <select bind:value={sessionTTLDraft} data-testid="session-preset">
              {#each ttlPresets as p}
                <option value={p.seconds}>{p.label}</option>
              {/each}
            </select>
          </label>
          <label class="pref-row">
            <span>Custom (seconds)</span>
            <input
              type="number"
              min="300"
              max="7776000"
              step="60"
              bind:value={sessionTTLDraft}
              data-testid="session-seconds"
            />
          </label>
          <div class="actions">
            <button on:click={saveSessionTTL} disabled={sessionBusy} data-testid="session-save">
              {sessionBusy ? "Saving…" : "Save"}
            </button>
          </div>
        {/if}

        {#if section === "email" && $user?.is_admin}
          <h3>Email / SMTP</h3>
          <p class="hint">
            Configure the relay used for daily digest emails. These fields override the
            corresponding <code>EMBER_SMTP_*</code> environment variables at runtime;
            changes take effect on the next digest tick (~5 minutes).
          </p>
          <div class="form-grid" style="display:grid;grid-template-columns:1fr 1fr;gap:10px 16px;">
            <label>
              SMTP host
              <input type="text" bind:value={emailDraft.host} placeholder="smtp.example.com" data-testid="smtp-host" />
            </label>
            <label>
              Port
              <input type="number" min="1" max="65535" bind:value={emailDraft.port} data-testid="smtp-port" />
            </label>
            <label>
              Username
              <input type="text" bind:value={emailDraft.username} autocomplete="off" data-testid="smtp-user" />
            </label>
            <label>
              Password
              {#if emailLoaded?.smtp.password_set && !emailDraft.password && !emailDraft.clear_password}
                <input type="password" bind:value={emailDraft.password} placeholder="•••• stored — leave blank to keep" autocomplete="new-password" data-testid="smtp-password" />
              {:else}
                <input type="password" bind:value={emailDraft.password} autocomplete="new-password" data-testid="smtp-password" />
              {/if}
            </label>
            <label style="grid-column: 1 / -1;">
              From address
              <input type="email" bind:value={emailDraft.from} placeholder="ember@example.com" data-testid="smtp-from" />
            </label>
          </div>
          <!-- STARTTLS is a toggle, not a value field — gets its own labeled
               row outside the input grid so it reads as on/off rather than
               "one weird item in a column of text inputs." -->
          <label class="toggle-row">
            <span class="toggle-label">
              <span class="toggle-title">Use STARTTLS</span>
              <span class="toggle-hint">Recommended for submission ports (587). Disable only when targeting a relay that doesn't support it.</span>
            </span>
            <span class="switch">
              <input type="checkbox" bind:checked={emailDraft.starttls} data-testid="smtp-starttls" />
              <span class="track" aria-hidden="true"></span>
            </span>
          </label>
          {#if emailLoaded?.smtp.password_set}
            <label class="check" style="margin-top:8px;">
              <input type="checkbox" bind:checked={emailDraft.clear_password} data-testid="smtp-clear-password" />
              Clear stored password on save
            </label>
          {/if}
          {#if emailMsg}<p class="ok" data-testid="email-msg">{emailMsg}</p>{/if}
          {#if emailErr}<p class="err" data-testid="email-err">{emailErr}</p>{/if}
          <div class="actions" style="margin-top:12px;">
            <button on:click={saveEmailSettings} disabled={emailBusy} data-testid="email-save">
              {emailBusy ? "Saving…" : "Save"}
            </button>
          </div>

          <hr style="margin:18px 0;border:0;border-top:1px solid var(--line);" />
          <h4>Send test email</h4>
          <p class="hint">Uses the live SMTP settings above. Save first if you've made changes.</p>
          <div style="display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap;">
            <label style="flex:1 1 240px;">
              Recipient (defaults to your account email)
              <input type="email" bind:value={testRecipient} placeholder="you@example.com" data-testid="smtp-test-to" />
            </label>
            <button on:click={sendTestEmail} disabled={testBusy} data-testid="smtp-test-send">
              {testBusy ? "Sending…" : "Send test"}
            </button>
          </div>

          <hr style="margin:18px 0;border:0;border-top:1px solid var(--line);" />
          <h4>Initial backlog window</h4>
          <p class="hint">
            When a new feed (or starter pack) is added, articles published more than this
            many hours ago are skipped. Subsequent polls of the feed are unaffected.
            Set to <code>0</code> to disable the gate and ingest a feed's full upstream history.
          </p>
          <label>
            Hours
            <input type="number" min="0" max="8760" bind:value={emailDraft.initial_backlog_hours} data-testid="backlog-hours" style="width:140px;" />
          </label>

          <div class="actions" style="margin-top:12px;">
            <button on:click={saveEmailSettings} disabled={emailBusy} data-testid="backlog-save">
              {emailBusy ? "Saving…" : "Save"}
            </button>
          </div>
        {/if}

        {#if section === "feeds"}
          <h3>Feeds</h3>
          <p class="hint">Server-wide controls for how Ember fetches your feeds. Admin only.</p>
          {#if feedErr}<p class="error" data-testid="feeds-error">{feedErr}</p>{/if}
          {#if feedMsg}<p class="ok" data-testid="feeds-msg">{feedMsg}</p>{/if}

          <h4>Feed check interval</h4>
          <p class="hint">
            How often Ember checks each feed for new articles. Active feeds settle near this
            value; quiet ones are checked less often. A longer interval keeps new items from
            piling up faster than you can read them. Allowed range: 5 minutes to 24 hours.
          </p>
          <label>
            Check feeds every
            <select bind:value={feedSettings.poll_min_interval_seconds} data-testid="poll-interval" style="width:160px;">
              {#each pollIntervalPresets as p}
                <option value={p.seconds}>{p.label}</option>
              {/each}
            </select>
          </label>

          <h4 style="margin-top:18px;">Reading window</h4>
          <p class="hint">
            Today, a feed, and a category show only articles published within this many hours
            (newest first). Older articles are kept for search but hidden from these views and
            from the unread counts. Range: {feedSettings.window_hours_floor}–{feedSettings.window_hours_ceil} hours
            (capped at the 1-week retention window).
          </p>
          <label>
            Show last
            <input type="number" min={feedSettings.window_hours_floor} max={feedSettings.window_hours_ceil}
              bind:value={feedSettings.reading_window_hours} data-testid="reading-window-hours" style="width:120px;" /> hours
          </label>

          <h4 style="margin-top:18px;">Search window</h4>
          <p class="hint">
            Full-text search matches articles published within this many hours. Default 48.
            It can't exceed the {feedSettings.window_hours_ceil}-hour retention window — you can't
            search what's already been pruned (the safeguard).
          </p>
          <label>
            Search last
            <input type="number" min={feedSettings.window_hours_floor} max={feedSettings.window_hours_ceil}
              bind:value={feedSettings.search_window_hours} data-testid="search-window-hours" style="width:120px;" /> hours
          </label>

          {#if feedMsg}<p class="hint" data-testid="feed-settings-msg">{feedMsg}</p>{/if}
          {#if feedErr}<p class="error" data-testid="feed-settings-err">{feedErr}</p>{/if}
          <div class="actions" style="margin-top:12px;">
            <button on:click={saveFeedSettings} disabled={feedBusy} data-testid="poll-interval-save">
              {feedBusy ? "Saving…" : "Save"}
            </button>
          </div>
        {/if}

        {#if section === "about"}
          <h3>About</h3>
          <dl class="kv">
            <dt>Version</dt>
            <dd>
              {#if $appVersion.startsWith("v")}
                {@const tag = $appVersion.split("-")[0]}
                <a
                  class="version-badge"
                  href={`https://github.com/brandonhon/ember/releases/tag/${tag}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  data-testid="about-version"
                >{$appVersion}</a>
              {:else}
                <span class="version-badge version-badge-dev" data-testid="about-version">{$appVersion}</span>
              {/if}
            </dd>
            <dt>Project</dt><dd><a href="https://github.com/brandonhon/ember" target="_blank" rel="noopener noreferrer">github.com/brandonhon/ember</a></dd>
            <dt>License</dt><dd><a href="https://github.com/brandonhon/ember/blob/main/LICENSE" target="_blank" rel="noopener noreferrer">MIT</a></dd>
          </dl>
        {/if}
      </div>
    </div>
  </div>

  {#if showFilters}
    <FilterManager onClose={() => (showFilters = false)} />
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
    min-width: 0;
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
  .nav-group { display: flex; flex-direction: column; gap: 2px; margin-bottom: 14px; }
  .nav-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 10.5px;
    font-weight: 700;
    letter-spacing: 0.13em;
    text-transform: uppercase;
    /* --ink-faint (not --gold): gold-on-paper is only 2.99:1, below WCAG AA
       4.5:1 for this small bold text (caught by the a11y e2e). ink-faint is
       ~5.1:1 and passes in light + dark themes. */
    color: var(--ink-faint);
    padding: 4px 12px 6px;
  }
  .nav-label::after {
    content: "";
    height: 1px;
    flex: 1;
    background: linear-gradient(90deg, var(--line), transparent);
  }
  /* Per-row glyphs are mobile-only; hidden on desktop so the rail stays a
     clean text list (shown again under .modal.mobile below). */
  .nav-ic { display: none; }
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
  .content { padding: 22px 26px; overflow-y: auto; min-width: 0; }

  /* Mobile (≤900px): full-screen takeover with drill-down navigation.
     The 180px+1fr grid would crush both columns on a phone, so the modal
     becomes single-pane: show .nav OR .content, never both. Driven by
     data-view on .layout. The drilled-in section gets a back chevron in
     the header (rendered in JSX). */
  .modal.mobile {
    width: 100%;
    height: 100%;
    max-height: 100vh;
    border-radius: 0;
    border: 0;
  }
  .modal.mobile header {
    padding: 14px 16px;
    gap: 6px;
  }
  .modal.mobile .back-btn {
    background: transparent;
    border: 0;
    padding: 6px 8px;
    margin-right: 2px;
    color: var(--ink);
    display: grid;
    place-items: center;
    border-radius: 8px;
    cursor: pointer;
  }
  .modal.mobile .back-btn:hover { background: var(--line-soft); }
  .modal.mobile h2 { font-size: 18px; flex: 1; }
  .modal.mobile .layout { grid-template-columns: 1fr; }
  .modal.mobile .layout[data-view="list"] .content,
  .modal.mobile .layout[data-view="detail"] .nav { display: none; }
  /* Mobile list view = grouped inset cards (label on paper, rounded card of
     rows with icon chips + chevron). iOS-grouped feel in ember's palette. */
  .modal.mobile .layout[data-view="list"] .nav {
    border-right: 0;
    padding: 10px 16px 28px;
    background: var(--paper);
    gap: 0;
  }
  .modal.mobile .nav-group { margin: 0 0 22px; gap: 0; }
  .modal.mobile .nav-label { color: var(--ink-faint); padding: 0 8px 8px; }
  .modal.mobile .nav-label::after { display: none; }
  .modal.mobile .nav-ic {
    display: grid;
    place-items: center;
    flex: none;
    width: 30px;
    height: 30px;
    border-radius: 9px;
    background: var(--ember-wash);
    color: var(--ember);
  }
  .modal.mobile .nav-ic :global(svg) { width: 17px; height: 17px; }
  .modal.mobile .nav-ic-admin { background: rgba(176, 125, 26, 0.16); color: var(--gold); }
  .modal.mobile .nav button {
    display: flex;
    align-items: center;
    gap: 13px;
    padding: 12px 36px 12px 14px;
    font-size: 15px;
    border-radius: 0;
    background-color: var(--card);
    border-left: 1px solid var(--line);
    border-right: 1px solid var(--line);
    /* Chevron affordance: "tap to open a sub-screen" (iOS-style drill-down). */
    background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23847a68' stroke-width='2'><path d='M9 6l6 6-6 6'/></svg>");
    background-repeat: no-repeat;
    background-position: right 14px center;
    background-size: 14px 14px;
  }
  .modal.mobile .nav button + button { border-top: 1px solid var(--line-soft); }
  .modal.mobile .nav button:first-of-type { border-top: 1px solid var(--line); border-radius: 16px 16px 0 0; }
  .modal.mobile .nav button:last-of-type { border-bottom: 1px solid var(--line); border-radius: 0 0 16px 16px; }
  .modal.mobile .nav button:active { background-color: var(--paper-2); }
  .modal.mobile .nav button.active { background-color: var(--ember-wash); }
  .modal.mobile .content { padding: 18px 16px 48px; }
  /* Detail view: stack actions as full-width tap targets. */
  .modal.mobile .actions { flex-direction: column; align-items: stretch; }
  .modal.mobile .actions button { width: 100%; padding: 13px; font-size: 15px; }

  /* Form scaffolding — collapse two-column grids and side-by-side rows so
     fields stop overflowing the screen on a phone. */
  @media (max-width: 600px) {
    .row { flex-direction: column; gap: 8px; }
    /* The inline form-grid at L1577 uses style="grid-template-columns:1fr 1fr"
       — override via attribute selector since we can't edit the style attr
       from here without touching every form section. */
    [style*="grid-template-columns:1fr 1fr"] { grid-template-columns: 1fr !important; }
  }

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
  /* Links inside settings copy use the brand link color, not the browser
     default blue/purple (which reads wrong on the warm + dark themes). */
  .hint a, dd a { color: var(--ember); font-weight: 600; text-decoration: none; }
  .hint a:hover, dd a:hover { text-decoration: underline; }
  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 12px;
    font-size: 12px;
  }
  label > span { color: var(--ink-faint); font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; font-size: 10.5px; }
  input[type="text"], input[type="password"], input[type="email"], input[type="number"] {
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
  /* Themed <select>. Native browser chrome would render OS-default greys
     (gray on macOS/Linux, white on Windows) regardless of theme; styling
     it requires appearance: none + a custom chevron. The chevron SVG uses
     a neutral muted stroke that reads OK on both light + dark backgrounds
     without needing per-theme overrides. */
  select {
    padding: 8px 32px 8px 11px;
    border: 1px solid var(--line);
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 13px;
    background-color: var(--paper);
    color: var(--ink);
    appearance: none;
    -webkit-appearance: none;
    background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='10' height='10' viewBox='0 0 10 10'><path fill='none' stroke='%23999' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round' d='M2.5 4l2.5 2.5 2.5-2.5'/></svg>");
    background-repeat: no-repeat;
    background-position: right 10px center;
    cursor: pointer;
  }
  select:focus { outline: none; border-color: var(--ember); box-shadow: 0 0 0 3px var(--ember-wash); }
  select option { background: var(--paper); color: var(--ink); }
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
    /* Equal width across all buttons in a group so the segmented control
       looks balanced regardless of label length (Off / Daily / Weekly /
       Monthly vary by 2-3 characters). flex: 1 distributes available space;
       min-width keeps single-char labels from collapsing. */
    flex: 1 1 0;
    min-width: 64px;
    padding: 5px 12px;
    font-size: 12px;
    font-weight: 600;
    color: var(--ink-faint);
    background: transparent;
    border: none;
    cursor: pointer;
    text-align: center;
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
  /* Standalone secondary button: same look as `.actions button.ghost` for the
     ghost buttons that live outside an `.actions` row (Copy key, Export OPML,
     Reset branding, Rotate inbox, Test push). Without this they fell back to
     the unstyled browser default — the reported "buttons don't match" bug. */
  .ghost {
    background: transparent;
    color: var(--ink);
    border: 1px solid var(--line);
    padding: 7px 14px;
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
  }
  .ghost:hover:not(:disabled) { background: var(--line-soft); }
  .ghost:disabled { opacity: 0.5; cursor: not-allowed; }

  /* Settings list rows (passkeys, push devices, etc.). Shared shell so a
     registered passkey and a push-subscribed device read as members of
     the same UI family. Title on the left, sub-line below it, action
     button right-aligned. */
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    border-top: 1px solid var(--line-soft);
  }
  .list-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--line-soft);
  }
  .list-title {
    font-family: var(--font-ui);
    font-size: 13.5px;
    font-weight: 600;
    color: var(--ink);
  }
  .list-sub {
    font-size: 12px;
    color: var(--ink-faint);
    margin-top: 2px;
  }
  /* Outlined destructive button. Matches the .danger pattern from the
     sidebar context menus (#b91c1c) but with the .actions button shape
     so it sits comfortably next to .actions button.ghost. */
  .btn-danger {
    background: transparent;
    color: #b91c1c;
    border: 1px solid #b91c1c;
    padding: 6px 13px;
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
    flex-shrink: 0;
  }
  .btn-danger:hover:not(:disabled) {
    background: #b91c1c;
    color: #fff;
  }
  .btn-danger:disabled { opacity: 0.5; cursor: not-allowed; }
  /* Import & Data section */
  .import-card {
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 12px;
    padding: 18px 20px;
    margin-bottom: 14px;
    box-shadow: var(--shadow-card);
  }
  .import-card h4 { margin: 0; font-family: var(--font-display); font-weight: 600; font-size: 16px; }
  .import-sub { color: var(--ink-faint); font-size: 13px; margin: 5px 0 14px; line-height: 1.5; }
  .import-seg {
    display: inline-flex;
    background: var(--paper-2);
    border-radius: 9px;
    padding: 3px;
    gap: 3px;
    margin-bottom: 14px;
  }
  .import-seg button {
    border: 0;
    background: transparent;
    font-family: var(--font-ui);
    font-size: 12.5px;
    font-weight: 600;
    color: var(--ink-faint);
    padding: 6px 13px;
    border-radius: 6px;
    cursor: pointer;
  }
  .import-seg button.on {
    background: var(--card);
    color: var(--ember);
    box-shadow: 0 1px 2px rgba(33, 29, 24, 0.08);
  }
  .import-checks { display: flex; gap: 18px; margin: 4px 0 12px; }
  .import-checks label.inline {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 7px;
    font-size: 13px;
    font-weight: 500;
  }
  .import-note {
    font-size: 12px;
    color: var(--ink-faint);
    background: var(--ember-wash);
    border-radius: 8px;
    padding: 9px 12px;
    line-height: 1.5;
    margin: 0 0 6px;
  }
  .import-note code {
    font-family: ui-monospace, monospace;
    background: var(--paper-2);
    padding: 0 4px;
    border-radius: 4px;
    color: var(--ember);
  }
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
  /* Toggle row: label + hint on the left, switch on the right. Used for
     boolean settings that don't fit the "value input" rhythm of the form
     grid above (currently STARTTLS in the SMTP section). */
  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 14px;
    margin-top: 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--paper-2);
    cursor: pointer;
  }
  .toggle-row:hover { background: var(--line-soft); }
  .toggle-label {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .toggle-title { font-weight: 600; font-size: 13.5px; color: var(--ink); }
  .toggle-hint { font-size: 12px; color: var(--ink-faint); line-height: 1.4; }
  .switch {
    position: relative;
    flex: 0 0 auto;
    width: 38px;
    height: 22px;
    display: inline-block;
  }
  .switch input {
    /* The actual checkbox sits invisibly on top of the track so clicks +
       keyboard focus + form-state still work. Visual is the .track span. */
    position: absolute;
    inset: 0;
    margin: 0;
    opacity: 0;
    cursor: pointer;
    z-index: 1;
  }
  .switch .track {
    position: absolute;
    inset: 0;
    border-radius: 999px;
    background: var(--line);
    transition: background 0.18s ease;
  }
  .switch .track::after {
    content: "";
    position: absolute;
    top: 2px;
    left: 2px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--paper);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.18);
    transition: transform 0.18s ease;
  }
  .switch input:checked + .track { background: var(--ember); }
  .switch input:checked + .track::after { transform: translateX(16px); }
  .switch input:focus-visible + .track {
    outline: 2px solid var(--ember);
    outline-offset: 2px;
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
  /* Version pill: tagged builds (vX.Y.Z) get a clickable badge linked to
     the GitHub release. Dev / SHA-only builds get a muted no-link badge
     so the visual still reads as "this is a version identifier" without
     implying a release page exists. */
  .version-badge {
    display: inline-block;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 12px;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--ember-wash);
    color: var(--ember);
    border: 1px solid color-mix(in srgb, var(--ember) 30%, transparent);
    text-decoration: none;
    transition: background 0.12s, color 0.12s, border-color 0.12s;
  }
  /* Invert on hover for a clearly-readable state (paper-colored text on a
     solid ember fill) rather than a low-contrast wash-on-wash. */
  a.version-badge:hover {
    background: var(--ember);
    color: var(--card);
    border-color: var(--ember);
    text-decoration: none;
  }
  .version-badge-dev {
    background: var(--line-soft);
    color: var(--ink-faint);
    border-color: var(--line);
  }

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
  /* Remove variant: muted background so it doesn't read as the primary CTA. */
  .pack-btn-remove {
    background: transparent;
    color: var(--ember);
    border: 1px solid var(--ember);
  }
  .pack-btn-remove:hover:not(:disabled) {
    background: var(--ember);
    color: #fff;
  }

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
