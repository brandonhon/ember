<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError } from "../lib/api";
  import { user } from "../lib/stores";
  import type { User } from "../lib/types";

  let { articleId, articleTitle, onClose }: {
    articleId: number;
    articleTitle: string;
    onClose: () => void;
  } = $props();

  let users = $state<User[]>([]);
  let toUserId = $state<number | "">("");
  let note = $state("");
  let error = $state("");
  let sent = $state(false);
  let busy = $state(false);

  onMount(async () => {
    try {
      const res = await api.listUsers();
      // Filter self out — UI doesn't allow self-share.
      users = (res.data ?? []).filter((u) => u.id !== $user?.id);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  });

  async function send() {
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

    {#if error}
      <p class="error" data-testid="share-error">{error}</p>
    {/if}

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
        <button on:click={send} disabled={busy || toUserId === ""} data-testid="share-send">
          {busy ? "Sending…" : "Send"}
        </button>
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
  .subject { color: var(--muted); margin: 0 0 1rem; font-size: 0.85rem; }
  .error { background: #fef2f2; color: #991b1b; border-radius: 4px; padding: 0.5rem 0.75rem; font-size: 0.85rem; }
  .ok { color: var(--accent); }
  label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85rem; margin-bottom: 0.75rem; }
  select, textarea, input {
    padding: 0.4rem 0.5rem; border: 1px solid var(--border);
    border-radius: 4px; font: inherit; background: var(--bg);
  }
  textarea { resize: vertical; }
  .actions { display: flex; justify-content: flex-end; gap: 0.5rem; }
  button {
    background: var(--accent); color: white; border: 0;
    padding: 0.4rem 0.85rem; border-radius: 4px; cursor: pointer; font: inherit;
  }
  button.ghost { background: transparent; color: inherit; border: 1px solid var(--border); }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  .muted { color: var(--muted); }
</style>
