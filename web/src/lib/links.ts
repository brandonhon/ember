// Force every link inside a rendered article body to open in a new browser
// tab. We pair target="_blank" with rel="noopener noreferrer" so the new tab
// can't reach back into this one (tab-napping). The server sanitizer already
// sets rel on feed content, but we re-assert it here so the rule also holds
// for any older articles sanitized before that policy and for the LLM-cleaned
// fallback body.
export function forceNewTabLinks(root: ParentNode): void {
  root.querySelectorAll("a[href]").forEach((a) => {
    a.setAttribute("target", "_blank");
    a.setAttribute("rel", "noopener noreferrer");
  });
}
