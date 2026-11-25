import { defineConfig } from 'vitepress';

const SITE = 'https://ember-rss.com';
const SOCIAL_ALT = 'Ember — a self-hosted RSS reader with local-first AI summaries';
// Pages that are published (linked from CHANGELOG.md) but kept out of search.
// Their own frontmatter adds `robots: noindex`; this keeps them out of the sitemap.
const NOINDEX = ['SECURITY_FINDINGS'];

// Canonical URL for a page: cleanUrls drops `.md`, and index.md is the root.
const pageUrl = (relativePath: string) =>
  `${SITE}/${relativePath.replace(/(^|\/)index\.md$/, '$1').replace(/\.md$/, '')}`;

// schema.org description of the project, emitted on the home page only.
const STRUCTURED_DATA = {
  '@context': 'https://schema.org',
  '@graph': [
    {
      '@type': 'WebSite',
      name: 'Ember',
      url: `${SITE}/`,
    },
    {
      '@type': 'SoftwareApplication',
      name: 'Ember',
      description:
        'Self-hosted RSS reader with a calm three-pane interface, cross-feed duplicate folding, rules, and optional local AI summaries.',
      url: `${SITE}/`,
      image: `${SITE}/social-preview.png`,
      screenshot: `${SITE}/screenshots/hero-2-threepane-summary-light.png`,
      applicationCategory: 'UtilitiesApplication',
      applicationSubCategory: 'RSS reader',
      operatingSystem: 'Linux, Docker',
      license: 'https://opensource.org/licenses/MIT',
      isAccessibleForFree: true,
      offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
      codeRepository: 'https://github.com/brandonhon/ember',
    },
  ],
};

// Base path: the site is served from the apex custom domain ember-rss.com,
// so assets resolve from the root. (When it was on brandonhon.github.io/ember/
// this had to be '/ember/'; the CNAME moves it to root.)
export default defineConfig({
  base: '/',
  lang: 'en-US',
  title: 'Ember',
  description: 'Self-hosted RSS reader with local-first AI summaries.',
  head: [
    // Self-adaptive favicon: the SVG contains its own
    // prefers-color-scheme <style> rules so a single <link> tag works in
    // both OS modes. Firefox historically ignored media-scoped favicon
    // <link>s; the in-SVG CSS sidesteps that entirely.
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/icon.svg' }],
    ['meta', { name: 'theme-color', content: '#a93b16' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'Ember' }],
    ['meta', { property: 'og:locale', content: 'en_US' }],
    // og:image/twitter:image must be ABSOLUTE URLs — social scrapers don't
    // resolve relative paths. social-preview.png lives in docs/public/ and is
    // copied to the site root on build.
    ['meta', { property: 'og:image', content: `${SITE}/social-preview.png` }],
    ['meta', { property: 'og:image:width', content: '1280' }],
    ['meta', { property: 'og:image:height', content: '640' }],
    ['meta', { property: 'og:image:alt', content: SOCIAL_ALT }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
    ['meta', { name: 'twitter:image', content: `${SITE}/social-preview.png` }],
    ['meta', { name: 'twitter:image:alt', content: SOCIAL_ALT }],
    // Per-page title/description/URL tags are added in transformHead below.
  ],
  cleanUrls: true,
  sitemap: {
    hostname: SITE,
    transformItems: (items) => items.filter((i) => !NOINDEX.some((p) => i.url.includes(p))),
  },
  // Per-page canonical + social tags, so a shared docs link unfurls with that
  // page's own title and description instead of the home page's.
  transformHead({ pageData, title, description }) {
    const url = pageUrl(pageData.relativePath);
    const head: [string, Record<string, string>, string?][] = [
      ['link', { rel: 'canonical', href: url }],
      ['meta', { property: 'og:url', content: url }],
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: description }],
    ];
    if (pageData.relativePath === 'index.md') {
      head.push(['script', { type: 'application/ld+json' }, JSON.stringify(STRUCTURED_DATA)]);
    }
    return head;
  },
  lastUpdated: true,
  themeConfig: {
    siteTitle: 'Ember',
    // Header logo: VitePress swaps automatically based on the active site
    // theme (light/dark mode toggle), independent of OS theme.
    logo: { light: '/icon.svg', dark: '/icon-dark.svg' },
    nav: [
      { text: 'Guide', link: '/getting-started' },
      { text: 'Architecture', link: '/architecture' },
      { text: 'Security', link: '/security' },
      { text: 'Screenshots', link: '/screenshots' },
      // Static demo SPA, built into /demo/ by pages.yml. Not a VitePress page,
      // so target=_blank avoids the SPA router fighting VitePress navigation.
      { text: 'Live Demo', link: '/demo/', target: '_blank', rel: 'noopener' },
      { text: 'GitHub', link: 'https://github.com/brandonhon/ember' },
      { text: 'Tangled', link: 'https://tangled.org/nodnarb.tngl.sh/ember' },
    ],
    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Introduction', link: '/' },
          { text: 'Getting started', link: '/getting-started' },
          { text: 'Configuration', link: '/configuration' },
          { text: 'Summarization', link: '/summarization' },
          { text: 'Hardening Caddy', link: '/caddy-hardening' },
          { text: 'Upgrading', link: '/upgrading' },
        ],
      },
      // Both shipped, so both are linked. Every docs/*.md is published
      // regardless (there is no srcExclude), so leaving a page unlinked hides
      // it from the sidebar without hiding it from search — which is how the
      // notifications page sat stale and reachable for several releases.
      {
        text: 'Features',
        items: [
          { text: 'Notifications', link: '/notifications' },
          { text: 'Email inbox', link: '/email-inbox' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'Architecture', link: '/architecture' },
          { text: 'Security', link: '/security' },
          { text: 'Screenshots', link: '/screenshots' },
        ],
      },
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/brandonhon/ember' }],
    search: { provider: 'local' },
    editLink: {
      pattern: 'https://github.com/brandonhon/ember/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
    footer: {
      message: 'Released under the MIT License.',
      copyright: '© 2026 Ember',
    },
  },
});
