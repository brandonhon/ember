<script lang="ts">
  import { demoNotice } from "../demo/demo";
  const docsHref = import.meta.env.BASE_URL.replace(/demo\/?$/, "") || "/";
  function close() { demoNotice.set(false); }
</script>

{#if $demoNotice}
  <div class="dn-scrim" role="presentation" on:click={close}>
    <div class="dn-card" role="dialog" aria-modal="true" aria-label="Demo notice" on:click|stopPropagation>
      <div class="dn-mark" aria-hidden="true">✦</div>
      <h2>This is a live demo</h2>
      <p>
        You're browsing a frozen snapshot — reading, starring and searching work,
        but changes aren't saved and management actions are disabled.
      </p>
      <p class="muted">Self-host Ember to add your own feeds, run the AI summaries, and keep everything.</p>
      <div class="dn-actions">
        <button class="dn-secondary" type="button" on:click={close}>Keep exploring</button>
        <a class="dn-primary" href={docsHref}>Self-host Ember →</a>
      </div>
    </div>
  </div>
{/if}

<style>
  .dn-scrim {
    position: fixed;
    inset: 0;
    z-index: 10000;
    display: grid;
    place-items: center;
    padding: 20px;
    background: color-mix(in srgb, var(--ink, #211d18) 55%, transparent);
    backdrop-filter: blur(2px);
  }
  .dn-card {
    width: 100%;
    max-width: 380px;
    background: var(--card, #fffdf8);
    color: var(--ink, #211d18);
    border: 1px solid var(--line, #d8cfb9);
    border-radius: 16px;
    padding: 26px 24px 20px;
    box-shadow: 0 24px 60px -20px rgba(0, 0, 0, 0.45);
    font-family: var(--font-ui, system-ui, sans-serif);
    text-align: center;
  }
  .dn-mark {
    font-size: 22px;
    color: var(--ember, #a93b16);
    margin-bottom: 8px;
  }
  .dn-card h2 {
    font-family: var(--font-display, Georgia, serif);
    font-size: 21px;
    font-weight: 600;
    margin: 0 0 10px;
  }
  .dn-card p { font-size: 14px; line-height: 1.55; margin: 0 0 10px; color: var(--ink-soft, var(--ink, #211d18)); }
  .dn-card p.muted { color: var(--ink-faint, #6a604f); font-size: 13px; }
  .dn-actions { display: flex; gap: 10px; margin-top: 18px; }
  .dn-secondary, .dn-primary {
    flex: 1;
    padding: 10px 12px;
    border-radius: 11px;
    font-size: 13.5px;
    font-weight: 600;
    font-family: inherit;
    cursor: pointer;
    text-decoration: none;
    text-align: center;
    border: 1px solid var(--line, #d8cfb9);
  }
  .dn-secondary { background: var(--card, #fffdf8); color: var(--ink, #211d18); }
  .dn-secondary:hover { border-color: var(--ember, #a93b16); }
  .dn-primary { background: var(--ember, #a93b16); color: #fff; border-color: var(--ember, #a93b16); }
  .dn-primary:hover { background: var(--ember-soft, #c2451d); }
</style>
