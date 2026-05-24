<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError } from "../lib/api";
  import { user, articles } from "../lib/stores";
  import type { User } from "../lib/types";

  let { articleId, articleTitle, onClose }: {
    articleId: number;
    articleTitle: string;
    onClose: () => void;
  } = $props();

  type Mode = "user" | "email" | "link";
  let mode = $state<Mode>("user");

  let users = $state<User[]>([]);
  let toUserId = $state<number | "">("");
  let note = $state("");
  let error = $state("");
  let sent = $state(false);
  let busy = $state(false);
  let copyMsg = $state("");
  let emailTo = $state("");

  // Derive article URL from the currently-selected article in the store.
  const articleURL = $derived.by(() => {
    const a = $articles.items.find((x) => x.id === articleId);
    return a?.url ?? "";
  });
  const articleSummary = $derived.by(() => {
    const a = $articles.items.find((x) => x.id === articleId);
    return a?.summary ?? "";
  });

  onMount(async () => {
    try {
      const res = await api.listUsers();
      users = (res.data ?? []).filter((u) => u.id !== $user?.id);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  });

  async function sendUser() {
    if (toUserId === "") {
      error = "select a recipient";
      return;
    }
    error = "";
    busy = true;
    try {
      await api.createShare(articleId, Number(toUserId), note.trim() || undefined);
      sent = true;
      setTimeout(onClose, 800);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  function sendEmail() {
    const subject = encodeURIComponent(articleTitle);
    const bodyLines = [articleTitle];
    if (articleSummary) {
      bodyLines.push("", articleSummary);
    }
    if (articleURL) {
      bodyLines.push("", articleURL);
    }
    if (note.trim()) {
      bodyLines.push("", "— " + note.trim());
    }
    const body = encodeURIComponent(bodyLines.join("\n"));
    const to = encodeURIComponent(emailTo.trim());
    location.href = `mailto:${to}?subject=${subject}&body=${body}`;
  }

  async function copyLink() {
    if (!articleURL) {
      copyMsg = "No URL to copy";
      return;
    }
    try {
      await navigator.clipboard.writeText(articleURL);
      copyMsg = "Link copied to clipboard";
    } catch {
      copyMsg = "Copy failed — please copy manually";
    }
  }
</script>

<div
  class="backdrop"
  role="dialog"
  aria-modal="true"
  aria-labelledby="share-modal-title"
  on:click={onClose}
  data-testid="share-modal"
>
  <div class="modal" on:click|stopPropagation>
    <header>
      <h2 id="share-modal-title">Share article</h2>
      <button class="close" on:click={onClose} aria-label="Close">×</button>
    </header>

    <p class="subject">{articleTitle}</p>

    <div class="tabs" role="tablist">
      <button role="tab" aria-selected={mode === "user"} class:on={mode === "user"} on:click={() => (mode = "user")} data-testid="share-tab-user">User</button>
      <button role="tab" aria-selected={mode === "email"} class:on={mode === "email"} on:click={() => (mode = "email")} data-testid="share-tab-email">Email</button>
      <button role="tab" aria-selected={mode === "link"} class:on={mode === "link"} on:click={() => (mode = "link")} data-testid="share-tab-link">Copy link</button>
    </div>

    {#if error}
      <p class="error" data-testid="share-error">{error}</p>
    {/if}

    {#if mode === "user"}
      {#if sent}
        <p class="ok">Sent.</p>
      {:else if users.length === 0}
        <p class="muted">No other users to share with.</p>
      {:else}
        <label>
          <span>To</span>
          <select bind:value={toUserId} data-testid="share-to">
            <option value="">— pick a user —</option>
            {#each users as u (u.id)}
              <option value={u.id}>{u.username}</option>
            {/each}
          </select>
        </label>
        <label>
          <span>Note (optional)</span>
          <textarea bind:value={note} rows="3" data-testid="share-note"></textarea>
        </label>
        <div class="actions">
          <button class="ghost" on:click={onClose}>Cancel</button>
          <button on:click={sendUser} disabled={busy || toUserId === ""} data-testid="share-send">
            {busy ? "Sending…" : "Send"}
          </button>
        </div>
      {/if}
    {:else if mode === "email"}
      <label>
        <span>Recipient email</span>
        <input type="email" bind:value={emailTo} placeholder="friend@example.com" data-testid="share-email-to" />
      </label>
      <label>
        <span>Note (optional)</span>
        <textarea bind:value={note} rows="3"></textarea>
      </label>
      <p class="hint">Opens your default mail client with the article title, summary, and link prefilled.</p>
      <div class="actions">
        <button class="ghost" on:click={onClose}>Cancel</button>
        <button on:click={sendEmail} data-testid="share-email-send">Compose email</button>
      </div>
    {:else if mode === "link"}
      <label>
        <span>Article link</span>
        <input type="text" value={articleURL} readonly data-testid="share-link-input" on:focus={(e) => (e.currentTarget as HTMLInputElement).select()} />
      </label>
      {#if copyMsg}<p class="ok" data-testid="share-link-msg">{copyMsg}</p>{/if}
      <div class="actions">
        <button class="ghost" on:click={onClose}>Close</button>
        <button on:click={copyLink} disabled={!articleURL} data-testid="share-link-copy">Copy link</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed; inset: 0;
    background: rgba(0, 0, 0, 0.45);
    display: flex; align-items: flex-start; justify-content: center;
    padding-top: 5rem; z-index: 100;
  }
  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    width: min(480px, 95vw);
    padding: 1.25rem 1.5rem;
    color: var(--text);
  }
  header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; }
  h2 { margin: 0; font-family: "Fraunces", serif; }
  .close { background: transparent; border: 0; font-size: 1.5rem; cursor: pointer; color: var(--muted); }
  .subject { color: var(--muted); margin: 0 0 0.75rem; font-size: 0.85rem; }
  .tabs {
    display: flex;
    gap: 4px;
    background: var(--paper-2);
    border-radius: 8px;
    padding: 3px;
    margin-bottom: 0.85rem;
  }
  .tabs button {
    flex: 1;
    background: transparent;
    color: var(--ink-soft);
    border: 0;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
    font-family: var(--font-ui);
  }
  .tabs button.on {
    background: var(--card);
    color: var(--ember);
    box-shadow: 0 1px 2px rgba(33, 29, 24, 0.06);
  }
  .error { background: #fef2f2; color: #991b1b; border-radius: 4px; padding: 0.5rem 0.75rem; font-size: 0.85rem; }
  .ok { color: var(--ember); font-size: 0.85rem; margin: 0 0 0.5rem; }
  .hint { color: var(--muted); font-size: 0.78rem; margin: 0 0 0.75rem; }
  label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85rem; margin-bottom: 0.75rem; }
  label > span { color: var(--muted); font-weight: 600; }
  select, textarea, input {
    padding: 0.4rem 0.5rem; border: 1px solid var(--border);
    border-radius: 4px; font: inherit; background: var(--bg);
    color: var(--text);
  }
  textarea { resize: vertical; }
  input[readonly] { background: var(--paper-2); }
  .actions { display: flex; justify-content: flex-end; gap: 0.5rem; }
  .actions button {
    background: var(--accent); color: white; border: 0;
    padding: 0.4rem 0.85rem; border-radius: 4px; cursor: pointer; font: inherit;
  }
  .actions button.ghost { background: transparent; color: inherit; border: 1px solid var(--border); }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  .muted { color: var(--muted); }
</style>
