<script lang="ts">
  import { onMount } from "svelte";
  import { login, refreshMe } from "../lib/stores";
  import { api, ApiError } from "../lib/api";
  import { getPasskey, passkeySupported } from "../lib/passkey";

  let username = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);
  let passkeyBusy = $state(false);

  // Cursor lands in the username field on mount and on every re-render of
  // the login screen (e.g. after the user signs out and we route them back
  // here). Bare HTML `autofocus` only fires on first paint and stops working
  // after Svelte un-mounts/re-mounts the component, so we drive focus from
  // an $effect against the bound element instead.
  let usernameEl: HTMLInputElement | undefined = $state();
  $effect(() => { usernameEl?.focus(); });
  // Two preconditions for showing the "Sign in with passkey" button:
  //   1) Browser supports WebAuthn (passkeySupported()).
  //   2) At least one passkey is registered on this server. Avoids dangling
  //      the option in front of fresh users on a brand-new install.
  // The server-wide check stays system-wide (not per-username) — see
  // /api/auth/passkey/exists for why.
  const browserSupportsPasskey = passkeySupported();
  let anyPasskeyRegistered = $state(false);
  onMount(async () => {
    if (!browserSupportsPasskey) return;
    try {
      const res = await api.passkeyAnyRegistered();
      anyPasskeyRegistered = !!res.data?.any_registered;
    } catch {
      // Network error / pre-login probe failure → leave button hidden.
      anyPasskeyRegistered = false;
    }
  });
  const canPasskey = $derived(browserSupportsPasskey && anyPasskeyRegistered);

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

  async function onPasskey() {
    if (!username) {
      error = "Type your username first";
      return;
    }
    passkeyBusy = true;
    error = "";
    try {
      const begin = await api.passkeyLoginBegin(username);
      const credential = await getPasskey(begin.data.options as any);
      await api.passkeyLoginFinish(begin.data.session_id, credential);
      await refreshMe();
    } catch (err) {
      if (err instanceof ApiError) {
        error = err.message || "Passkey sign-in failed";
      } else if (err instanceof DOMException) {
        error = "Passkey sign-in cancelled";
      } else {
        error = String(err);
      }
    } finally {
      passkeyBusy = false;
    }
  }
</script>

<div id="login">
  <div class="login-brand">
    <div class="mark">
      <svg width="28" height="28" viewBox="0 0 64 64" aria-hidden="true">
        <defs>
          <linearGradient id="login-mark-emb" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stop-color="var(--ember-soft)" />
            <stop offset="1" stop-color="var(--ember)" />
          </linearGradient>
        </defs>
        <circle cx="13" cy="15" r="6.5" fill="url(#login-mark-emb)" />
        <rect x="25" y="11.5" width="31" height="8" rx="4" fill="var(--paper)" />
        <rect x="8" y="28" width="48" height="8" rx="4" fill="var(--paper)" />
        <rect x="8" y="44.5" width="34" height="8" rx="4" fill="url(#login-mark-emb)" />
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
          bind:this={usernameEl}
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

      {#if canPasskey}
        <div class="or">or</div>
        <button
          type="button"
          class="btn-secondary"
          on:click={onPasskey}
          disabled={passkeyBusy || !username}
          data-testid="login-passkey"
        >
          {passkeyBusy ? "Waiting for passkey…" : "Sign in with passkey"}
        </button>
      {/if}

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
    gap: 4px;
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
    /* Was rgba(...,0.6) — too soft against --ink on narrow viewports where
       the panel is still visible (e.g. phone landscape between 800 and
       1100px). 0.82 keeps the "subdued tagline" look without forcing
       readers to squint. */
    color: rgba(246, 242, 233, 0.82);
    max-width: 38ch;
    line-height: 1.6;
    margin-top: 18px;
    font-size: 15px;
  }
  @media (max-width: 1100px) {
    /* Between the 800px panel-hide breakpoint and ~1100px the brand panel
       shares the screen with the form card and gets narrow — the H1 and
       tagline crowd each other. Tighten padding and bump body text so the
       tagline stays legible on phone-landscape and small tablets. */
    .login-brand { padding: 36px 32px; }
    .message h1 { font-size: clamp(30px, 5vw, 44px); }
    .message p { font-size: 15.5px; line-height: 1.55; }
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

  .or {
    text-align: center;
    color: var(--ink-faint);
    font-size: 12px;
    margin: 14px 0 8px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .btn-secondary {
    width: 100%;
    padding: 11px;
    border-radius: 11px;
    background: var(--card);
    color: var(--ink);
    font-weight: 600;
    font-size: 14px;
    border: 1px solid var(--line);
    cursor: pointer;
    font-family: var(--font-ui);
    transition: border-color 0.15s, background 0.15s;
  }
  .btn-secondary:hover:not(:disabled) { border-color: var(--ember); }
  .btn-secondary:disabled { opacity: 0.5; cursor: not-allowed; }

  .login-meta {
    margin-top: 18px;
    font-size: 12px;
    color: var(--ink-faint);
    text-align: center;
  }
</style>
