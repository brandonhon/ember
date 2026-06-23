import { defineConfig } from 'vitepress';

// Base path: GitHub Pages serves under /ember/. Configured here so all
// links + asset URLs resolve correctly when the site lands on
// brandonhon.github.io/ember/.
export default defineConfig({
  base: '/ember/',
  lang: 'en-US',
  title: 'Ember',
  description: 'Self-hosted RSS reader with on-device AI summaries.',
  head: [
    // Self-adaptive favicon: the SVG contains its own
    // prefers-color-scheme <style> rules so a single <link> tag works in
    // both OS modes. Firefox historically ignored media-scoped favicon
    // <link>s; the in-SVG CSS sidesteps that entirely.
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/ember/icon.svg' }],
    ['meta', { name: 'theme-color', content: '#a93b16' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'Ember' }],
    ['meta', { property: 'og:description', content: 'Self-hosted RSS reader with on-device AI summaries.' }],
    // og:image/twitter:image must be ABSOLUTE URLs — social scrapers don't
    // resolve relative paths or the /ember/ base. social-preview.png lives in
    // docs/public/ and is copied to the site root on build.
    ['meta', { property: 'og:image', content: 'https://brandonhon.github.io/ember/social-preview.png' }],
    ['meta', { property: 'og:url', content: 'https://brandonhon.github.io/ember/' }],
    ['meta', { property: 'og:site_name', content: 'Ember' }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
    ['meta', { name: 'twitter:title', content: 'Ember' }],
    ['meta', { name: 'twitter:description', content: 'Self-hosted RSS reader with on-device AI summaries.' }],
    ['meta', { name: 'twitter:image', content: 'https://brandonhon.github.io/ember/social-preview.png' }],
  ],
  cleanUrls: true,
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
          { text: 'Hardening Caddy', link: '/caddy-hardening' },
          { text: 'Upgrading', link: '/upgrading' },
        ],
      },
      // Features section hidden until these ship in a release — Web Push
      // and the email newsletter inbox are still in development. The
      // notifications.md / email-inbox.md pages stay in the repo, just
      // unlinked from the sidebar. Re-enable when they land in a tagged release.
      // {
      //   text: 'Features',
      //   items: [
      //     { text: 'Notifications', link: '/notifications' },
      //     { text: 'Email inbox', link: '/email-inbox' },
      //   ],
      // },
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
