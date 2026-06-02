import { h } from 'vue';
import DefaultTheme from 'vitepress/theme';
import AiOptionalCallout from './AiOptionalCallout.vue';
import './style.css';

// Wraps the default theme's Layout component with two extra slot uses:
//   - `home-features-before` renders the "AI is fully optional" band
//     between the hero and the features grid (markdown :::tip blocks
//     in index.md land *below* the features grid, which buried the
//     callout — surfacing it up top makes the AI-opt-out message
//     load-bearing for new visitors).
//
// Every other behavior is the VitePress default; this is the smallest
// override that still gets the layout we want.
export default {
  extends: DefaultTheme,
  Layout: () =>
    h(DefaultTheme.Layout, null, {
      'home-features-before': () => h(AiOptionalCallout),
    }),
};
