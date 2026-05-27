// Favicon overlay helper: takes a base favicon URL and renders it onto an
// offscreen canvas, optionally drawing a small filled circle in the top-right
// corner to signal unread/new articles (matches the TT-RSS visual pattern).
//
// Returns a data: URL the caller can drop into <link rel="icon"> directly.
//
// Cross-origin caveat: if the base favicon is on a different origin and the
// server doesn't return CORS headers, canvas.toDataURL() throws SecurityError
// due to canvas tainting. We catch it and return the original URL — the link
// gets swapped without the dot rather than failing outright.

type CacheKey = string;
const cache = new Map<CacheKey, string>();

const SIZE = 64;
// Dot proportions: small, clean, bottom-right corner — mirrors the OS-level
// app badge that Chrome/Edge/Safari render for installed PWAs via the
// Badging API. The earlier ~30%-diameter top-right design felt heavy; this
// is ~22% with a thin contrast ring so it pops on busy favicons without
// dominating them.
const DOT_R = 11;
const HALO_R = 14;
const DOT_X = SIZE - 14;
const DOT_Y = SIZE - 14;
const DOT_COLOR = "#4f7a3d"; // matches --green in the default palette
const HALO_COLOR = "rgba(255, 255, 255, 0.95)";

function key(baseURL: string, dot: boolean): CacheKey {
  return `${baseURL}|${dot ? 1 : 0}`;
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.crossOrigin = "anonymous"; // gives the server a chance to grant CORS
    img.onload = () => resolve(img);
    img.onerror = (e) => reject(e);
    img.src = src;
  });
}

export async function renderFaviconWithDot(
  baseURL: string,
  dot: boolean,
): Promise<string> {
  const k = key(baseURL, dot);
  const cached = cache.get(k);
  if (cached) return cached;

  // SSR / non-browser test environments (jsdom in Vitest) lack a real canvas.
  // Skip the rasterization in that case; the link href just stays at the base.
  if (typeof document === "undefined") return baseURL;

  let img: HTMLImageElement;
  try {
    img = await loadImage(baseURL);
  } catch {
    // Couldn't even load the base. Fall back so the favicon link still works.
    return baseURL;
  }

  const canvas = document.createElement("canvas");
  canvas.width = SIZE;
  canvas.height = SIZE;
  const ctx = canvas.getContext("2d");
  if (!ctx) return baseURL;

  ctx.drawImage(img, 0, 0, SIZE, SIZE);

  if (dot) {
    // Halo first (light ring) so the dot pops on dark and light favicons alike.
    ctx.fillStyle = HALO_COLOR;
    ctx.beginPath();
    ctx.arc(DOT_X, DOT_Y, HALO_R, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = DOT_COLOR;
    ctx.beginPath();
    ctx.arc(DOT_X, DOT_Y, DOT_R, 0, Math.PI * 2);
    ctx.fill();
  }

  try {
    const url = canvas.toDataURL("image/png");
    cache.set(k, url);
    return url;
  } catch {
    // Tainted canvas (cross-origin without CORS). Degrade gracefully.
    return baseURL;
  }
}

// Test/debug helper. Not exported from the package's barrel.
export function _clearFaviconCache(): void {
  cache.clear();
}
