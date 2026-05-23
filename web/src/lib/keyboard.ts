// Keyboard shortcuts for the reader. Pure mapping function so we can test
// without a DOM, plus an attach() to wire it up.

export type ShortcutAction =
  | "next"
  | "prev"
  | "open-original"
  | "toggle-read"
  | "toggle-star"
  | "refresh"
  | "focus-search"
  | "show-help";

export interface KeyEvent {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
  shiftKey?: boolean;
  target?: { tagName?: string };
}

const TYPING_TAGS = new Set(["INPUT", "TEXTAREA", "SELECT"]);

// shortcutFor maps a key event to an action, or null. We deliberately skip
// shortcuts while typing in form fields except for `/` (focus search) which
// is intercepted before this fires.
export function shortcutFor(e: KeyEvent): ShortcutAction | null {
  if (e.ctrlKey || e.metaKey || e.altKey) return null;
  const tag = e.target?.tagName?.toUpperCase() ?? "";
  if (TYPING_TAGS.has(tag)) {
    // Allow Escape to bubble (caller's responsibility), no shortcut action.
    return null;
  }
  switch (e.key) {
    case "j":
      return "next";
    case "k":
      return "prev";
    case "o":
      return "open-original";
    case "m":
      return "toggle-read";
    case "s":
      return "toggle-star";
    case "r":
      return "refresh";
    case "/":
      return "focus-search";
    case "?":
      return "show-help";
    default:
      return null;
  }
}

// attach wires the keymap to the global window. Returns a cleanup function.
export function attach(
  handle: (action: ShortcutAction, e: KeyboardEvent) => void,
): () => void {
  const listener = (e: KeyboardEvent) => {
    const act = shortcutFor({
      key: e.key,
      ctrlKey: e.ctrlKey,
      metaKey: e.metaKey,
      altKey: e.altKey,
      shiftKey: e.shiftKey,
      target: e.target as { tagName?: string },
    });
    if (act) {
      e.preventDefault();
      handle(act, e);
    }
  };
  window.addEventListener("keydown", listener);
  return () => window.removeEventListener("keydown", listener);
}
