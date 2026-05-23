<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError } from "../lib/api";
  import { user } from "../lib/stores";
  import type { User } from "../lib/types";

  let { onClose }: { onClose: () => void } = $props();

  let users = $state<User[]>([]);
  let loading = $state(false);
  let error = $state("");

  let newUsername = $state("");
  let newEmail = $state("");
  let newPassword = $state("");
  let newIsAdmin = $state(false);

  async function refresh() {
    loading = true;
    error = "";
    try {
      const res = await api.listUsers();
      users = res.data ?? [];
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  async function createUser() {
    if (!newUsername.trim() || !newPassword.trim()) {
      error = "username and password required";
      return;
    }
    error = "";
    try {
      await api.createUser({
        username: newUsername.trim(),
        email: newEmail.trim() || undefined,
        password: newPassword,
        is_admin: newIsAdmin,
      });
      newUsername = "";
      newEmail = "";
      newPassword = "";
      newIsAdmin = false;
      await refresh();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function deleteUser(u: User) {
    if (!confirm(`Delete user "${u.username}"? This cannot be undone.`)) return;
    try {
      // Direct fetch — no api.deleteUser since user-delete is admin-only and
      // we share the same CSRF/cookie path. The existing api client doesn't
      // expose a DELETE for users yet; inline it here.
      const tok = document.cookie.match(/ember_csrf=([^;]+)/)?.[1] ?? "";
      const res = await fetch(`/api/users/${u.id}`, {
        method: "DELETE",
        credentials: "include",
        headers: tok ? { "X-Ember-CSRF": decodeURIComponent(tok) } : {},
      });
      if (!res.ok) {
        throw new Error(`delete user: ${res.status}`);
      }
      await refresh();
    } catch (e) {
      error = String(e);
    }
  }
</script>

<div
  class="backdrop"
  role="dialog"
  aria-modal="true"
  aria-labelledby="manage-users-title"
  on:click={onClose}
  data-testid="manage-users"
>
  <div class="modal" on:click|stopPropagation>
    <header>
      <h2 id="manage-users-title">Users</h2>
      <button class="close" on:click={onClose} aria-label="Close">×</button>
    </header>

    {#if error}
      <p class="error" data-testid="users-error">{error}</p>
    {/if}

    <section class="form">
      <h3>New user</h3>
      <div class="row">
        <label>
          <span>Username</span>
          <input bind:value={newUsername} autocomplete="username" data-testid="new-user-username" />
        </label>
        <label>
          <span>Email (optional)</span>
          <input bind:value={newEmail} type="email" autocomplete="email" />
        </label>
      </div>
      <div class="row">
        <label>
          <span>Password</span>
          <input bind:value={newPassword} type="password" autocomplete="new-password" data-testid="new-user-password" />
        </label>
        <label class="checkbox">
          <input type="checkbox" bind:checked={newIsAdmin} />
          <span>Admin</span>
        </label>
      </div>
      <div class="actions">
        <button type="button" on:click={createUser} data-testid="create-user-submit">
          Create user
        </button>
      </div>
    </section>

    <hr />

    <section>
      <h3>Existing users</h3>
      {#if loading}
        <p class="muted">Loading…</p>
      {:else}
        <ul>
          {#each users as u (u.id)}
            <li data-testid="user-row-{u.id}">
              <div class="info">
                <strong>{u.username}</strong>
                {#if u.email}<span class="muted">— {u.email}</span>{/if}
                {#if u.is_admin}<span class="badge">admin</span>{/if}
              </div>
              <div>
                {#if $user?.id !== u.id}
                  <button class="ghost danger" on:click={() => deleteUser(u)}>Delete</button>
                {:else}
                  <span class="muted">(you)</span>
                {/if}
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 5rem;
    z-index: 100;
  }
  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    width: min(640px, 95vw);
    max-height: 80vh;
    overflow-y: auto;
    padding: 1.25rem 1.5rem;
    color: var(--text);
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.75rem;
  }
  h2 { margin: 0; font-family: "Fraunces", serif; }
  h3 { font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); margin: 0.5rem 0; }
  .close { background: transparent; border: 0; font-size: 1.5rem; cursor: pointer; color: var(--muted); }
  .error {
    background: #fef2f2; color: #991b1b;
    border-radius: 4px; padding: 0.5rem 0.75rem; font-size: 0.85rem;
  }
  .form { display: flex; flex-direction: column; gap: 0.5rem; }
  .row { display: flex; gap: 0.75rem; align-items: flex-end; }
  .row label { flex: 1; display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85rem; }
  label.checkbox { flex: 0 0 auto; flex-direction: row; align-items: center; gap: 0.4rem; }
  input {
    padding: 0.4rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    font: inherit;
  }
  .actions { display: flex; justify-content: flex-end; }
  button {
    background: var(--accent); color: white; border: 0;
    padding: 0.4rem 0.85rem; border-radius: 4px; cursor: pointer; font: inherit;
  }
  button.ghost {
    background: transparent; color: inherit;
    border: 1px solid var(--border);
  }
  button.ghost.danger { color: #b91c1c; border-color: #fecaca; }
  hr { margin: 1rem 0; border: 0; border-top: 1px solid var(--border); }
  ul { list-style: none; padding: 0; margin: 0; }
  li {
    display: flex; justify-content: space-between; align-items: center;
    padding: 0.5rem 0; border-bottom: 1px solid var(--border);
  }
  .info { display: flex; align-items: center; gap: 0.5rem; }
  .badge {
    background: var(--accent-bg); color: var(--accent);
    padding: 0.05rem 0.4rem; border-radius: 4px;
    font-size: 0.7rem; text-transform: uppercase;
  }
  .muted { color: var(--muted); font-size: 0.85rem; }
</style>
