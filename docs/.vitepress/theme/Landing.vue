<!--
  Home page ("try it first" layout). Rendered from index.md under
  `layout: page`, so VitePress supplies only the nav and footer; every
  section below is ours. Colours come from the theme tokens in style.css,
  so the page follows the docs' light/dark toggle like every other page.

  Screenshots are theme-inverted on purpose (same convention as the old
  hero and screenshots.md): a dark capture on the light page, a light
  capture on the dark page, so the image contrasts with the background.
-->
<script setup lang="ts">
import { computed, ref } from 'vue';
import { withBase } from 'vitepress';

const DEMO = '/demo/';
const shot = (name: string) => withBase(`/screenshots/${name}`);

type Tab = {
  id: string;
  label: string;
  title: string;
  body: string;
  link: string;
  href: string;
  // [shown on the light page, shown on the dark page]; null = markup panel
  img: [string, string] | null;
  alt: string;
};

const tabs: Tab[] = [
  {
    id: 'reader',
    label: 'Reader',
    title: 'Three panes. Zero clutter.',
    body: 'Folders on the left, the day’s articles in the middle, one focused article on the right. j/k to move, s to star, m to mark read — your hands never leave the keyboard.',
    link: 'Open it in the demo →',
    href: DEMO,
    img: ['hero-2-threepane-summary-dark.png', 'hero-2-threepane-summary-light.png'],
    alt: 'Ember three-pane reader with an AI summary card above the article',
  },
  {
    id: 'summaries',
    label: 'AI summaries',
    title: 'A paragraph and the key points. On your hardware.',
    body: 'Pull and swap Ollama models from the admin screen, or point at any OpenAI-compatible endpoint or Claude instead. Summaries also strip newsletter and podcast promos from the article body. Don’t want them? One switch turns them off.',
    link: 'How summaries work →',
    href: '/summarization',
    img: ['settings-llm-desktop-dark.png', 'settings-llm-desktop-light.png'],
    alt: 'Admin screen for choosing and tuning the summary model',
  },
  {
    id: 'rules',
    label: 'Rules',
    title: 'Tell it once. It remembers.',
    body: 'Match on feed, tags, title, date or whether there’s an image — then mark read, star, hide, tag or add to a board. Preview shows how many of the last week’s articles a rule would catch before you save it.',
    link: 'Try a rule in the demo →',
    href: DEMO,
    img: null,
    alt: '',
  },
  {
    id: 'themes',
    label: 'Themes & mobile',
    title: 'Looks right on every screen.',
    body: 'Light, dark, Solarized, sepia, Nord, Gruvbox and high contrast — or derive a whole palette from three colours you pick. The same reader works on your phone.',
    link: 'See all screenshots →',
    href: '/screenshots',
    img: ['reader-mobile-dark.png', 'reader-mobile-light.png'],
    alt: 'Ember article list on a phone',
  },
];

const active = ref(tabs[0].id);
const current = computed(() => tabs.find((t) => t.id === active.value) ?? tabs[0]);
// The image pair stays mounted while the markup-only Rules panel shows.
const mediaImg = computed(() => current.value.img ?? tabs[0].img!);
const tabEls = ref<HTMLButtonElement[]>([]);

// Arrow keys move between tabs (WAI-ARIA tabs pattern).
function onTabKey(e: KeyboardEvent, i: number) {
  const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
  if (!step) return;
  e.preventDefault();
  const next = (i + step + tabs.length) % tabs.length;
  active.value = tabs[next].id;
  tabEls.value[next]?.focus();
}

const INSTALL = `git clone https://github.com/brandonhon/ember.git
cd ember/deploy && cp .env.example .env
# set EMBER_SESSION_KEY and EMBER_ADMIN_PASSWORD in .env
docker compose up -d`;

const copied = ref(false);
async function copyInstall() {
  try {
    await navigator.clipboard.writeText(INSTALL);
    copied.value = true;
    setTimeout(() => (copied.value = false), 2000);
  } catch {
    // Clipboard blocked (insecure context / permissions): the text is
    // still selectable in the block below.
  }
}
</script>

<template>
  <div class="landing">
    <!-- Hero -->
    <section class="hero wrap">
      <a class="pill" href="https://github.com/brandonhon/ember/releases">
        <span class="pill-tag">Free &amp; open source</span>
        <span class="pill-extra">MIT licensed ·</span> see what’s new →
      </a>
      <h1 class="hero-title">Your feeds. Your server.<br /><span class="accent">Your summaries.</span></h1>
      <p class="lede">
        A self-hosted RSS reader with a calm, three-pane interface and optional AI summaries
        that run on your own hardware. Click around the real thing before you install anything.
      </p>
      <div class="actions">
        <a class="btn btn-primary" :href="withBase(DEMO)" target="_blank" rel="noopener">
          Open the live demo
          <svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M13 6l6 6-6 6" /></svg>
        </a>
        <a class="btn btn-ghost" href="#install">Self-host it</a>
      </div>
      <p class="fine">No sign-up · runs in your browser · nothing to install</p>
    </section>

    <!-- Product shot: browser frame + phone overlay (phone alone on small screens) -->
    <div class="showcase wrap">
      <div class="browser">
        <div class="browser-bar" aria-hidden="true">
          <span class="dot" /><span class="dot" /><span class="dot" />
          <span class="url">ember.your-homelab.net</span>
        </div>
        <img class="light-only" :src="shot('hero-2-threepane-summary-dark.png')" alt="Ember three-pane reader with an AI summary card above the article" width="2880" height="1800" loading="lazy" decoding="async" />
        <img class="dark-only" :src="shot('hero-2-threepane-summary-light.png')" alt="Ember three-pane reader with an AI summary card above the article" width="2880" height="1800" loading="lazy" decoding="async" />
      </div>
      <div class="phone">
        <img class="light-only" :src="shot('reader-mobile-dark.png')" alt="The same reader on a phone" width="780" height="1688" loading="lazy" decoding="async" />
        <img class="dark-only" :src="shot('reader-mobile-light.png')" alt="The same reader on a phone" width="780" height="1688" loading="lazy" decoding="async" />
      </div>
    </div>

    <!-- Who it's for -->
    <section id="who" class="wrap section">
      <h2 class="section-title center">Pick your path</h2>
      <div class="paths">
        <div class="card">
          <svg aria-hidden="true" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" class="icon"><path d="M2 4h7a3 3 0 0 1 3 3v13a2 2 0 0 0-2-2H2zM22 4h-7a3 3 0 0 0-3 3v13a2 2 0 0 1 2-2h8z" /></svg>
          <h3>“I just want a better reader.”</h3>
          <ul>
            <li>Distraction-free reading with keyboard shortcuts</li>
            <li>Duplicate wire stories folded into one row</li>
            <li>Full-text search and saved searches</li>
            <li>Bring your feeds: OPML from any reader, or a full Tiny Tiny RSS migration</li>
            <li>Works on your phone — or keep Reeder &amp; co. via the Fever API</li>
          </ul>
          <a class="more" :href="withBase(DEMO)" target="_blank" rel="noopener">Try it in the demo →</a>
        </div>
        <div class="card">
          <svg aria-hidden="true" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" class="icon"><rect x="3" y="4" width="18" height="7" rx="2" /><rect x="3" y="13" width="18" height="7" rx="2" /><path d="M7 7.5h.01M7 16.5h.01" /></svg>
          <h3>“I run a homelab.”</h3>
          <ul>
            <li>One ~25 MB Go binary, pure-Go SQLite, no CGO</li>
            <li>Ollama out of the box; OpenAI-compatible or Claude if you prefer</li>
            <li>Passkeys, argon2id, CSRF protection, SSRF-blocked fetches</li>
            <li>Scheduled backups, OPML exports and a daily digest email</li>
          </ul>
          <a class="more" :href="withBase('/architecture')">Read the architecture →</a>
        </div>
      </div>
    </section>

    <!-- Feature tour -->
    <section id="tour" class="tour">
      <div class="wrap section">
        <div class="center">
          <h2 class="section-title">A quick tour</h2>
          <p class="sub">Four things you’ll notice in the first five minutes.</p>
        </div>
        <div class="tablist" role="tablist" aria-label="Feature tour">
          <button
            v-for="(t, i) in tabs"
            :key="t.id"
            :id="`tab-${t.id}`"
            ref="tabEls"
            class="tab"
            role="tab"
            type="button"
            :aria-selected="active === t.id"
            :aria-controls="`panel-${t.id}`"
            :tabindex="active === t.id ? 0 : -1"
            @click="active = t.id"
            @keydown="onTabKey($event, i)"
          >{{ t.label }}</button>
        </div>
        <div class="panel" role="tabpanel" :id="`panel-${current.id}`" :aria-labelledby="`tab-${current.id}`">
          <div class="panel-copy">
            <h3>{{ current.title }}</h3>
            <p>{{ current.body }}</p>
            <a class="more" :href="withBase(current.href)" :target="current.href === DEMO ? '_blank' : undefined" :rel="current.href === DEMO ? 'noopener' : undefined">{{ current.link }}</a>
          </div>
          <div class="panel-media" :class="{ narrow: current.id === 'themes' }">
            <!-- v-show, not v-if/v-else: swapping branches after SSR hydration
                 rendered the rule card's static children empty. -->
            <div v-show="current.img" class="media-imgs">
              <img class="light-only" :src="shot(mediaImg[0])" :alt="current.alt" loading="lazy" decoding="async" />
              <img class="dark-only" :src="shot(mediaImg[1])" :alt="current.alt" loading="lazy" decoding="async" />
            </div>
            <div v-show="!current.img" class="rule">
              <div class="rule-head"><strong>Rule · Hide sponsored posts</strong><span>Priority 1</span></div>
              <div class="rule-row">When <b>title</b> contains <code>sponsored</code></div>
              <div class="rule-row">Then <b>mark_read</b> and <b>tag</b> “ads”</div>
              <div class="rule-foot">Preview counts last-7-day matches before you save</div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Trust strip -->
    <section class="wrap trust">
      <div class="trust-item">
        <svg aria-hidden="true" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="icon"><path d="M3 11l9-8 9 8v9a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z" /></svg>
        <div><strong>Runs on your box</strong><p>No account, no cloud service.</p></div>
      </div>
      <div class="trust-item">
        <svg aria-hidden="true" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="icon"><rect x="4" y="4" width="16" height="16" rx="3" /><path d="M9 9h6v6H9z" /></svg>
        <div><strong>Local AI, optional</strong><p>Ollama keeps articles on your host. One switch turns it off.</p></div>
      </div>
      <div class="trust-item">
        <svg aria-hidden="true" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="icon"><path d="M12 3l8 4v5c0 5-3.5 8-8 9-4.5-1-8-4-8-9V7z" /></svg>
        <div><strong>Hardened by default</strong><p><a :href="withBase('/security')">Read the security model</a></p></div>
      </div>
      <div class="trust-item">
        <svg aria-hidden="true" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="icon"><path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M8 21H5a2 2 0 0 1-2-2v-3M16 21h3a2 2 0 0 0 2-2v-3" /></svg>
        <div><strong>MIT licensed</strong><p>Fork it, patch it, keep it.</p></div>
      </div>
    </section>

    <!-- Install -->
    <section id="install" class="install">
      <h2 class="section-title">Liked the demo? Three commands.</h2>
      <div class="code">
        <div class="code-bar">
          <span>docker compose</span>
          <button type="button" class="copy" @click="copyInstall" :aria-label="copied ? 'Copied' : 'Copy install commands'">{{ copied ? 'Copied' : 'Copy' }}</button>
        </div>
        <pre><code>{{ INSTALL }}</code></pre>
      </div>
      <p class="sub">
        Open <code>https://localhost</code>, log in, pick a starter pack — articles show up within a minute.
        <a :href="withBase('/getting-started')">Full setup guide →</a>
      </p>
    </section>

    <!-- Closing CTA -->
    <section class="closer">
      <div class="wrap closer-inner">
        <h2>See if it fits how you read.</h2>
        <a class="btn btn-dark" :href="withBase(DEMO)" target="_blank" rel="noopener">Open the live demo</a>
      </div>
    </section>
  </div>
</template>

<style scoped>
.landing {
  --ld-on-brand: #fffdf8;
  --ld-code-bg: #211d18;
  --ld-code-fg: #f0e9da;
  font-family: var(--vp-font-family-base);
  color: var(--vp-c-text-1);
  background: var(--vp-c-bg);
}
.dark .landing {
  --ld-on-brand: #15130f;
  --ld-code-bg: #0e0c0a;
}

.wrap { max-width: 1200px; margin: 0 auto; padding-left: 24px; padding-right: 24px; }
.section { padding-top: 88px; padding-bottom: 88px; }
.center { text-align: center; }
.icon { color: var(--vp-c-brand-1); flex: none; }

h1, h2, h3 { margin: 0; border: 0; padding: 0; }
.section-title { font-family: 'Fraunces', Georgia, serif; font-size: 40px; line-height: 1.1; font-weight: 600; }
.sub { margin: 12px 0 0; font-size: 18px; line-height: 1.6; color: var(--vp-c-text-2); }

a.more, .trust a, .install a { color: var(--vp-c-brand-1); font-weight: 600; text-decoration: none; }
a.more:hover, .trust a:hover, .install a:hover { text-decoration: underline; }

/* Hero */
.hero { padding-top: 72px; padding-bottom: 48px; display: flex; flex-direction: column; align-items: center; gap: 24px; text-align: center; }
.pill { display: inline-flex; gap: 10px; align-items: center; border: 1px solid var(--vp-c-border); border-radius: 999px; padding: 6px 14px 6px 6px; font-size: 14px; color: var(--vp-c-text-2); text-decoration: none; }
.pill:hover { border-color: var(--vp-c-brand-1); }
.pill-tag { white-space: nowrap; background: var(--vp-c-brand-soft); color: var(--vp-c-brand-1); border-radius: 999px; padding: 3px 10px; font-weight: 600; }
.hero-title { font-family: 'Fraunces', Georgia, serif; font-weight: 600; font-size: clamp(40px, 6.4vw, 84px); line-height: 1.04; letter-spacing: -0.02em; }
.accent { color: var(--vp-c-brand-1); }
.lede { margin: 0; max-width: 680px; font-size: 20px; line-height: 1.55; color: var(--vp-c-text-2); }
.actions { display: flex; flex-wrap: wrap; gap: 12px; justify-content: center; padding-top: 8px; }
.btn { display: inline-flex; align-items: center; justify-content: center; gap: 10px; min-height: 52px; padding: 0 28px; border-radius: 12px; font-weight: 700; font-size: 18px; text-decoration: none; transition: background-color 0.15s, border-color 0.15s; }
.btn-primary { background: var(--vp-c-brand-1); color: var(--ld-on-brand); }
.btn-primary:hover { background: var(--vp-c-brand-2); }
.btn-ghost { color: var(--vp-c-text-1); border: 1px solid var(--vp-c-border); font-weight: 600; }
.btn-ghost:hover { background: var(--vp-c-bg-soft); }
.fine { margin: 0; font-size: 14px; color: var(--vp-c-text-2); }

/* Showcase */
.showcase { position: relative; padding-bottom: 96px; }
.browser { margin-right: 8%; border-radius: 14px; overflow: hidden; border: 1px solid var(--vp-c-border); background: var(--vp-c-bg-elv); box-shadow: 0 40px 120px -40px rgba(232, 100, 58, 0.45); }
.browser-bar { background: var(--vp-c-bg-soft); padding: 12px 16px; display: flex; align-items: center; gap: 8px; }
.dot { width: 11px; height: 11px; border-radius: 50%; background: var(--vp-c-border); }
.url { margin-left: 16px; flex: 1; background: var(--vp-c-bg); border-radius: 6px; padding: 5px 12px; font-size: 13px; color: var(--vp-c-text-2); }
.browser img { display: block; width: 100%; height: auto; }
.phone { position: absolute; z-index: 2; right: 24px; bottom: 48px; width: 21%; max-width: 250px; }
.phone img { display: block; width: 100%; height: auto; border-radius: 28px; border: 8px solid var(--vp-c-bg-soft); box-shadow: 0 30px 80px -20px rgba(0, 0, 0, 0.55); }

/* Paths */
.paths { margin-top: 32px; display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 420px), 1fr)); gap: 20px; }
.card { background: var(--vp-c-bg-alt); border: 1px solid var(--vp-c-divider); border-radius: 16px; padding: 36px; display: flex; flex-direction: column; gap: 18px; }
.card h3 { font-family: var(--vp-font-family-base); font-size: 24px; font-weight: 700; }
.card ul { margin: 0; padding-left: 20px; display: flex; flex-direction: column; gap: 10px; font-size: 17px; line-height: 1.5; color: var(--vp-c-text-2); list-style: disc; }
.card .more { margin-top: auto; }

/* Tour */
.tour { background: var(--vp-c-bg-alt); border-top: 1px solid var(--vp-c-divider); border-bottom: 1px solid var(--vp-c-divider); }
.tablist { margin: 36px 0; display: flex; flex-wrap: wrap; justify-content: center; gap: 8px; }
.tab { font: inherit; font-size: 16px; font-weight: 600; min-height: 44px; padding: 0 20px; border-radius: 999px; cursor: pointer; background: transparent; color: var(--vp-c-text-2); border: 1px solid var(--vp-c-border); }
.tab:hover { color: var(--vp-c-text-1); }
.tab[aria-selected='true'] { background: var(--vp-c-text-1); color: var(--vp-c-bg); border-color: var(--vp-c-text-1); }
.tab:focus-visible { outline: 2px solid var(--vp-c-brand-1); outline-offset: 2px; }
.panel { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr)); gap: 48px; align-items: center; }
.panel-copy { display: flex; flex-direction: column; gap: 16px; }
.panel-copy h3 { font-family: 'Fraunces', Georgia, serif; font-size: 32px; line-height: 1.15; font-weight: 600; }
.panel-copy p { margin: 0; font-size: 18px; line-height: 1.6; color: var(--vp-c-text-2); }
.panel-media { border-radius: 12px; overflow: hidden; border: 1px solid var(--vp-c-border); background: var(--vp-c-bg); min-height: 280px; display: flex; align-items: center; justify-content: center; }
.media-imgs { width: 100%; display: flex; justify-content: center; }
/* The settings screenshot is ~1:1.7; show its top, not a 1000px-tall panel. */
.panel-media img { display: block; width: 100%; height: auto; max-height: 520px; object-fit: cover; object-position: top; }
.panel-media.narrow { padding: 24px 0; }
.panel-media.narrow img { width: auto; max-width: 60%; max-height: 520px; border-radius: 20px; }
.rule { width: 100%; box-sizing: border-box; padding: 32px; display: flex; flex-direction: column; gap: 14px; font-size: 15px; }
.rule-head { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.rule-head strong { font-size: 17px; }
.rule-head span, .rule-foot { color: var(--vp-c-text-2); font-size: 14px; }
.rule-row { background: var(--vp-c-bg-alt); border: 1px solid var(--vp-c-border); border-radius: 8px; padding: 14px; }
.rule-row b { color: var(--vp-c-brand-1); }
.rule code { background: var(--vp-c-bg-soft); padding: 2px 6px; border-radius: 4px; font-size: 14px; }

/* Trust */
.trust { padding-top: 64px; padding-bottom: 64px; display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr)); gap: 24px; }
.trust-item { display: flex; gap: 14px; align-items: flex-start; }
.trust-item p { margin: 4px 0 0; font-size: 15px; line-height: 1.5; color: var(--vp-c-text-2); }

/* Install */
.install { max-width: 960px; margin: 0 auto; padding: 32px 24px 96px; display: flex; flex-direction: column; gap: 20px; }
.code { background: var(--ld-code-bg); border: 1px solid var(--vp-c-divider); border-radius: 12px; overflow: hidden; }
.code-bar { display: flex; justify-content: space-between; align-items: center; padding: 6px 6px 6px 16px; border-bottom: 1px solid rgba(240, 233, 218, 0.12); font-size: 13px; color: #b8ac98; }
.copy { background: rgba(240, 233, 218, 0.1); color: var(--ld-code-fg); border: 0; border-radius: 8px; min-height: 44px; padding: 0 16px; font: inherit; font-weight: 600; cursor: pointer; }
.copy:hover { background: rgba(240, 233, 218, 0.18); }
.code pre { margin: 0; padding: 20px; overflow-x: auto; }
.code code { font-family: var(--vp-font-family-mono); font-size: 15px; line-height: 1.7; color: var(--ld-code-fg); white-space: pre; }
.install .sub code { font-family: var(--vp-font-family-mono); font-size: 15px; background: var(--vp-c-bg-soft); padding: 2px 6px; border-radius: 4px; }

/* Closer */
.closer { background: var(--vp-c-brand-1); color: var(--ld-on-brand); }
.closer-inner { padding-top: 64px; padding-bottom: 64px; display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 24px; }
.closer h2 { font-family: 'Fraunces', Georgia, serif; font-size: 40px; font-weight: 600; line-height: 1.1; max-width: 640px; color: inherit; }
.btn-dark { background: var(--vp-c-text-1); color: var(--vp-c-bg); }
.btn-dark:hover { opacity: 0.9; }

/* Phones: drop the desktop browser frame, show the phone screenshot alone. */
@media (max-width: 639px) {
  .hero { padding-top: 40px; align-items: stretch; text-align: left; }
  .pill { align-self: flex-start; font-size: 13px; }
  .pill-extra { display: none; }
  .lede { font-size: 17px; }
  .actions { flex-direction: column; }
  .btn { width: 100%; box-sizing: border-box; }
  .fine { text-align: center; }
  .browser { display: none; }
  .showcase { padding-bottom: 56px; display: flex; justify-content: center; }
  .phone { position: static; width: 250px; max-width: 70%; }
  .section { padding-top: 56px; padding-bottom: 56px; }
  .section-title, .closer h2 { font-size: 30px; }
  .card { padding: 24px; }
  .card h3 { font-size: 20px; }
  .tablist { flex-wrap: nowrap; justify-content: flex-start; overflow-x: auto; margin: 24px -24px; padding: 0 24px 4px; }
  .tab { flex: none; white-space: nowrap; }
  .panel { gap: 24px; }
  .panel-copy h3 { font-size: 24px; }
  .trust { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px 16px; }
  .trust-item { flex-direction: column; gap: 6px; }
  .code code { font-size: 13px; }
}
</style>
