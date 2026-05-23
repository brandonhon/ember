<script lang="ts">
  import { login } from "../lib/stores";
  import { ApiError } from "../lib/api";

  let username = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);

  async function onSubmit(e: Event) {
    e.preventDefault();
    if (!username || !password) return;
    busy = true;
    error = "";
    try {
      await login(username, password);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        error = "Invalid username or password";
      } else {
        error = String(err);
      }
    } finally {
      busy = false;
    }
  }
</script>

<div class="login">
  <form on:submit={onSubmit}>
    <h1>Ember</h1>
    <p class="tagline">self-hosted RSS reader</p>
    <label>
      Username
      <input type="text" bind:value={username} autocomplete="username" data-testid="username" />
    </label>
    <label>
      Password
      <input type="password" bind:value={password} autocomplete="current-password" data-testid="password" />
    </label>
    {#if error}
      <p class="error" data-testid="login-error">{error}</p>
    {/if}
    <button type="submit" disabled={busy} data-testid="login-submit">
      {busy ? "Signing in…" : "Sign in"}
    </button>
  </form>
</div>

<style>
  .login {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    background: var(--bg);
  }
  form {
    width: 320px;
    padding: 2rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  h1 {
    margin: 0;
    font-family: "Fraunces", serif;
  }
  .tagline {
    color: var(--muted);
    margin: -0.25rem 0 0.5rem;
  }
  label {
    display: flex;
    flex-direction: column;
    font-size: 0.85rem;
    gap: 0.25rem;
  }
  input {
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    font: inherit;
  }
  button {
    margin-top: 0.5rem;
    padding: 0.65rem;
    background: var(--accent);
    color: white;
    border: 0;
    border-radius: 4px;
    cursor: pointer;
    font: inherit;
  }
  button:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .error {
    color: #b91c1c;
    margin: 0;
    font-size: 0.85rem;
  }
</style>
