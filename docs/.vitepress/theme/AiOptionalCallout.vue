<!--
  "AI is fully optional" band injected between the hero and the features
  grid on the home page. Lives in its own slot so the markdown body of
  index.md stays clean and the callout doesn't get pushed below the
  features list (which is where a markdown ::: tip block would render).
-->
<template>
  <div class="ai-optional-band">
    <div class="ai-optional-inner">
      <span class="ai-optional-icon" aria-hidden="true">💡</span>
      <p>
        <strong>AI is fully optional.</strong>
        The summary card, model picker, and Ollama sidecar are an opt-out
        feature, not a dependency. Set <code>EMBER_DISABLE_SUMMARIES=1</code>
        (or run the stack without the <code>ollama</code> container) and the
        reader works exactly the same — minus the summary card. No model
        download, no inference, no LLM code paths run. Even when enabled,
        everything stays on your box; no article content leaves the host.
      </p>
    </div>
  </div>
</template>

<style scoped>
.ai-optional-band {
  /* Sits between the VitePress hero and feature grid. The .VPHome
     content area caps at 1152px; we mirror that so the band stays
     aligned with the columns above and below.
     Outer margin top/bottom gives the band room — without it the
     VPFeatures grid below renders flush against the callout (the
     features section uses padding-top:0 on its inner container). */
  max-width: 1152px;
  margin: 24px auto 48px;
  padding: 0 24px;
}
.ai-optional-inner {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  background: var(--vp-c-brand-soft);
  border-left: 4px solid var(--vp-c-brand-1);
  border-radius: 0 12px 12px 0;
  padding: 20px 24px;
}
.ai-optional-icon {
  font-size: 22px;
  line-height: 1.2;
  flex-shrink: 0;
}
.ai-optional-inner p {
  margin: 0;
  font-size: 15px;
  line-height: 1.6;
  color: var(--vp-c-text-2);
}
.ai-optional-inner strong {
  color: var(--vp-c-text-1);
}
.ai-optional-inner code {
  background: var(--vp-c-bg-soft);
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 0.9em;
}
</style>
