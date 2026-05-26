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
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/ember/icon.svg' }],
    ['meta', { name: 'theme-color', content: '#a93b16' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'Ember' }],
    ['meta', { property: 'og:description', content: 'Self-hosted RSS reader with on-device AI summaries.' }],
  ],
  cleanUrls: true,
  lastUpdated: true,
  themeConfig: {
    siteTitle: 'Ember',
    logo: '/icon.svg',
    nav: [
      { text: 'Guide', link: '/getting-started' },
      { text: 'Architecture', link: '/architecture' },
      { text: 'Security', link: '/security' },
      { text: 'Screenshots', link: '/screenshots' },
      { text: 'GitHub', link: 'https://github.com/brandonhon/ember' },
    ],
    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Introduction', link: '/' },
          { text: 'Getting started', link: '/getting-started' },
          { text: 'Configuration', link: '/configuration' },
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
      copyright: '© 2026 Brandon Honeycutt',
    },
  },
});
