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

<div id="login">
  <div class="login-brand">
    <div class="mark">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
        <path d="M12 2L21 11L12 14L9 21L12 2Z" fill="var(--ember-soft)" />
        <path d="M12 2L3 11L12 14L12 2Z" fill="var(--paper)" opacity=".85" />
      </svg>
      Ember
    </div>

    <div class="message">
      <h1>Your feeds, <em>distilled</em> and self&#8209;hosted.</h1>
      <p>
        A calm, single-binary RSS reader. Folders like Feedly, story cards like
        Kite, summaries from a local model that never leaves your server.
      </p>
    </div>

    <div class="foot">SELF-HOSTED · GO · NO TRACKING</div>
  </div>

  <div class="login-form">
    <form class="login-card" on:submit={onSubmit}>
      <h2>Welcome back</h2>
      <div class="sub">Sign in to your reading space.</div>

      <div class="field">
        <label for="login-username">Username</label>
        <input
          id="login-username"
          type="text"
          bind:value={username}
          autocomplete="username"
          data-testid="username"
        />
      </div>
      <div class="field">
        <label for="login-password">Password</label>
        <input
          id="login-password"
          type="password"
          bind:value={password}
          autocomplete="current-password"
          data-testid="password"
        />
      </div>

      {#if error}
        <p class="login-error" data-testid="login-error">{error}</p>
      {/if}

      <button class="btn-primary" type="submit" disabled={busy} data-testid="login-submit">
        {busy ? "Signing in…" : "Sign in"}
      </button>

      <div class="login-meta">
        Multi-user · ask your admin for an invite
      </div>
    </form>
  </div>
</div>

<style>
  #login {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: grid;
    grid-template-columns: 1.1fr 0.9fr;
    background: var(--paper);
    font-family: var(--font-ui);
  }
  @media (max-width: 800px) {
    #login { grid-template-columns: 1fr; }
    .login-brand { display: none; }
  }

  /* LEFT — ink brand panel ------------------------------------------------ */
  .login-brand {
    background: var(--ink);
    color: var(--paper);
    padding: 52px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    position: relative;
    overflow: hidden;
  }
  .login-brand::after {
    content: "";
    position: absolute;
    right: -120px;
    bottom: -120px;
    width: 420px;
    height: 420px;
    border-radius: 50%;
    background: radial-gradient(circle at 30% 30%, var(--ember-soft), transparent 62%);
    opacity: 0.55;
    pointer-events: none;
  }

  .mark {
    display: flex;
    align-items: center;
    gap: 12px;
    font-family: var(--font-display);
    font-weight: 600;
    font-size: 20px;
  }

  .message { position: relative; z-index: 1; }
  .message h1 {
    font-family: var(--font-display);
    font-weight: 500;
    font-size: clamp(34px, 4vw, 54px);
    line-height: 1.04;
    letter-spacing: -0.02em;
    max-width: 12ch;
    margin: 0;
  }
  .message h1 em {
    font-style: italic;
    color: var(--ember-soft);
  }
  .message p {
    color: rgba(246, 242, 233, 0.6);
    max-width: 38ch;
    line-height: 1.6;
    margin-top: 18px;
    font-size: 13.5px;
  }

  .foot {
    font-size: 11px;
    color: rgba(246, 242, 233, 0.4);
    letter-spacing: 0.04em;
    font-weight: 600;
  }

  /* RIGHT — sign-in card -------------------------------------------------- */
  .login-form {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 40px;
    background: var(--paper);
  }
  .login-card {
    width: 100%;
    max-width: 340px;
    display: flex;
    flex-direction: column;
    gap: 0;
  }
  .login-card h2 {
    font-family: var(--font-display);
    font-size: 26px;
    font-weight: 500;
    letter-spacing: -0.01em;
    color: var(--ink);
    margin: 0;
  }
  .login-card .sub {
    color: var(--ink-faint);
    font-size: 13px;
    margin: 6px 0 24px;
  }

  .field { margin-bottom: 14px; }
  .field label {
    display: block;
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--ink-faint);
    margin-bottom: 6px;
    font-weight: 600;
  }
  .field input {
    width: 100%;
    padding: 11px 13px;
    border-radius: 11px;
    border: 1px solid var(--line);
    background: var(--card);
    color: var(--ink);
    font-family: var(--font-ui);
    font-size: 14px;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .field input:focus {
    outline: none;
    border-color: var(--ember);
    box-shadow: 0 0 0 3px var(--ember-wash);
  }

  .login-error {
    color: #b91c1c;
    font-size: 12.5px;
    margin: 4px 0 12px;
    background: #fef2f2;
    padding: 8px 11px;
    border-radius: 8px;
  }

  .btn-primary {
    width: 100%;
    padding: 11px;
    border-radius: 11px;
    background: var(--ember);
    color: #fff;
    font-weight: 600;
    font-size: 14px;
    letter-spacing: 0.01em;
    border: none;
    cursor: pointer;
    transition: transform 0.1s, background 0.15s;
    font-family: var(--font-ui);
  }
  .btn-primary:hover:not(:disabled) { background: var(--ember-soft); }
  .btn-primary:active:not(:disabled) { transform: translateY(1px); }
  .btn-primary:disabled { opacity: 0.6; cursor: not-allowed; }

  .login-meta {
    margin-top: 18px;
    font-size: 12px;
    color: var(--ink-faint);
    text-align: center;
  }
</style>
