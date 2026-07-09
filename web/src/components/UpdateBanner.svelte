<script lang="ts">
  // Admin-only "update available" strip. The server gates $updateInfo to admins
  // (readers get null), so this never shows for non-admins. Dismissal is
  // remembered per-version in localStorage — a newer release re-shows it.
  import { updateInfo } from "../lib/stores";

  const DISMISS_KEY = "ember:update-dismissed";
  let dismissedVersion =
    typeof localStorage !== "undefined" ? localStorage.getItem(DISMISS_KEY) ?? "" : "";

  $: info = $updateInfo;
  $: show = !!info?.available && dismissedVersion !== info.latest;

  function dismiss() {
    if (!info) return;
    dismissedVersion = info.latest;
    try {
      localStorage.setItem(DISMISS_KEY, info.latest);
    } catch {
      /* private-mode / storage disabled — dismiss for this session only */
    }
  }
</script>

{#if show && info}
  <div class="update-banner" role="status" data-testid="update-banner">
    <span class="dot" aria-hidden="true"></span>
    <span class="txt">
      <strong>Ember {info.latest} is available</strong>
      <span class="muted">— you're on {info.current}</span>
    </span>
    <a
      class="cta"
      href={info.url}
      target="_blank"
      rel="noopener noreferrer"
      data-testid="update-banner-link"
    >Release&nbsp;notes&nbsp;→</a>
    <button
      class="close"
      on:click={dismiss}
      aria-label="Dismiss update notice"
      data-testid="update-banner-dismiss"
    >×</button>
  </div>
{/if}

<style>
  .update-banner {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 16px;
    background: color-mix(in srgb, var(--ember, #e8643a) 12%, var(--paper, #f6f2e9));
    border-bottom: 1px solid color-mix(in srgb, var(--ember, #e8643a) 30%, transparent);
    color: var(--ink, #211d18);
    font-family: var(--font-ui, system-ui, sans-serif);
    font-size: 13px;
    line-height: 1.2;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--ember, #e8643a);
    flex: 0 0 auto;
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--ember, #e8643a) 28%, transparent);
  }
  .txt {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .muted {
    opacity: 0.7;
  }
  .cta {
    margin-left: auto;
    flex: 0 0 auto;
    color: var(--ember, #e8643a);
    text-decoration: none;
    font-weight: 600;
  }
  .cta:hover {
    text-decoration: underline;
  }
  .close {
    flex: 0 0 auto;
    background: none;
    border: none;
    color: var(--ink, #211d18);
    font-size: 20px;
    line-height: 1;
    cursor: pointer;
    padding: 0 2px;
    opacity: 0.6;
  }
  .close:hover {
    opacity: 1;
  }
  @media (max-width: 520px) {
    .muted {
      display: none;
    }
  }
</style>
