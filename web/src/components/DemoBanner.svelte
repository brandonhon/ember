<script lang="ts">
  import { DEMO_DATE } from "../demo/demo";
  // Link back to the docs/landing site (gh-pages root). In the demo build the
  // app is served at /ember/demo/, so ../ lands on /ember/.
  const docsHref = import.meta.env.BASE_URL.replace(/demo\/?$/, "") || "/";
</script>

<div class="demo-banner" role="note" aria-label="Demo mode">
  <span class="dot" aria-hidden="true"></span>
  <span class="txt">
    <strong>Demo mode</strong>
    <span class="muted">— data frozen{DEMO_DATE ? ` as of ${DEMO_DATE}` : ""}; changes reset on reload</span>
  </span>
  <a class="cta" href={docsHref}>Self-host&nbsp;→</a>
</div>

<style>
  .demo-banner {
    position: fixed;
    left: 50%;
    bottom: 16px;
    transform: translateX(-50%);
    z-index: 9999;
    display: flex;
    align-items: center;
    gap: 9px;
    max-width: calc(100vw - 24px);
    padding: 8px 14px;
    border-radius: 999px;
    background: var(--ink, #211d18);
    color: var(--paper, #f6f2e9);
    border: 1px solid color-mix(in srgb, var(--paper, #f6f2e9) 18%, transparent);
    box-shadow: 0 8px 28px -10px rgba(0, 0, 0, 0.5);
    font-family: var(--font-ui, system-ui, sans-serif);
    font-size: 12.5px;
    line-height: 1;
    white-space: nowrap;
    overflow: hidden;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--ember, #e8643a);
    flex: 0 0 auto;
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--ember, #e8643a) 28%, transparent);
  }
  .txt { overflow: hidden; text-overflow: ellipsis; }
  .muted { opacity: 0.7; }
  .cta {
    flex: 0 0 auto;
    color: var(--paper, #f6f2e9);
    text-decoration: none;
    font-weight: 600;
    padding: 4px 10px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--ember, #e8643a) 88%, black);
    border: none;
  }
  .cta:hover { background: var(--ember, #e8643a); }
  @media (max-width: 520px) {
    .demo-banner { font-size: 11.5px; bottom: 10px; gap: 7px; padding: 7px 11px; }
    .muted { display: none; }
  }
</style>
