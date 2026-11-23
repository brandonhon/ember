import DefaultTheme from 'vitepress/theme';
import './style.css';

// The home page is a self-contained component (Landing.vue, imported by
// index.md under `layout: page`); every other page is the VitePress default.
export default {
  extends: DefaultTheme,
};
