// Browser-side Web Push enrollment. Handles the dance of:
//   1) Make sure the service worker is registered.
//   2) Fetch the server's VAPID public key.
//   3) Ask the user for notification permission.
//   4) Call pushManager.subscribe with the VAPID key.
//   5) POST the resulting subscription to /api/me/push-subscriptions so
//      the server can send pushes to it later.
//
// Returns the new subscription id on success, or throws with a
// human-readable error on failure.

import { api, ApiError } from "./api";

// The server deliberately has no delete-by-endpoint route — that would let
// anyone purge a subscription by guessing endpoints — and the list response
// omits endpoints for the same reason. So the browser can't work out which of
// the listed rows is its own. It doesn't need to: enablePush already gets the
// id back, so remember it here and delete by id when switching off.
const SUB_ID_KEY = "ember:push-sub-id";

function rememberSubID(id: number): void {
  try {
    if (id > 0) localStorage.setItem(SUB_ID_KEY, String(id));
  } catch {
    // Private mode / storage disabled: disabling still unsubscribes this
    // browser, the row just has to be revoked from the device list instead.
  }
}
export function storedPushSubID(): number {
  try {
    return Number(localStorage.getItem(SUB_ID_KEY)) || 0;
  } catch {
    return 0;
  }
}
export function forgetPushSubID(): void {
  try {
    localStorage.removeItem(SUB_ID_KEY);
  } catch {
    /* nothing to forget */
  }
}

// Whether THIS browser currently holds a push subscription, which is what
// decides between offering Enable and Disable. Distinct from "this account has
// registered devices" — those may all be other browsers.
export async function pushSubscribedHere(): Promise<boolean> {
  if (!pushSupported()) return false;
  const reg = await navigator.serviceWorker.getRegistration();
  if (!reg) return false;
  return (await reg.pushManager.getSubscription()) !== null;
}

export function pushSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  );
}

export async function enablePush(): Promise<number> {
  if (!pushSupported()) {
    throw new Error("Push notifications aren't supported on this browser.");
  }
  // Re-use an existing SW registration or create one. The shell already
  // registers /sw.js at boot, but on first ever load this code might
  // race the registration — getRegistration awaits it.
  let reg = await navigator.serviceWorker.getRegistration();
  if (!reg) {
    reg = await navigator.serviceWorker.register("/sw.js");
  }
  await navigator.serviceWorker.ready;

  // Fetch the server's VAPID public key BEFORE asking for permission so
  // a misconfigured server (push disabled) doesn't trigger a permission
  // prompt the user can't satisfy.
  const keyRes = await api.pushVapidKey();
  const vapidKey = keyRes.data?.public_key;
  if (!vapidKey) {
    throw new Error("Push notifications are not configured on the server.");
  }

  const perm = await Notification.requestPermission();
  if (perm !== "granted") {
    throw new Error("Notification permission was denied.");
  }

  // If there's already a subscription for this browser, reuse it. The
  // server's CreatePushSubscription upserts on endpoint, so re-POSTing
  // an existing one just updates user_agent — safe.
  let sub = await reg.pushManager.getSubscription();
  if (!sub) {
    sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      // `applicationServerKey`'s typing accepts BufferSource, but TypeScript's
      // narrowed Uint8Array overload (with the generic ArrayBufferLike) trips
      // the assignability check. Cast the buffer view explicitly.
      applicationServerKey: urlBase64ToUint8Array(vapidKey).buffer as ArrayBuffer,
    });
  }

  const json = sub.toJSON();
  const endpoint = json.endpoint ?? sub.endpoint;
  const p256dh = json.keys?.p256dh ?? "";
  const auth = json.keys?.auth ?? "";
  if (!endpoint || !p256dh || !auth) {
    throw new Error("Browser returned an incomplete subscription.");
  }
  const res = await api.pushSubscribe({
    endpoint,
    p256dh,
    auth,
    user_agent: navigator.userAgent || "",
  });
  const id = res.data?.id ?? 0;
  rememberSubID(id);
  return id;
}

// Turn push off for this browser: drop the server row so nothing is sent, and
// unsubscribe locally so the browser stops holding a live subscription.
//
// The local unsubscribe runs even if the server call fails — a device the user
// asked to switch off must stop being subscribed regardless, and a row left
// behind is reaped by the server on the next 410. Returns false when the row
// couldn't be identified, so the caller can point the user at the device list.
export async function disablePush(): Promise<boolean> {
  const id = storedPushSubID();
  let serverCleared = id > 0;
  if (id > 0) {
    try {
      await api.pushUnsubscribe(id);
    } catch (err) {
      // Already gone (revoked from another device, or pruned) is a success
      // for our purposes; anything else the caller should hear about.
      if (!(err instanceof ApiError)) throw err;
      if (err.status !== 404) serverCleared = false;
    }
  }
  forgetPushSubID();

  const reg = await navigator.serviceWorker.getRegistration();
  const sub = reg ? await reg.pushManager.getSubscription() : null;
  if (sub) await sub.unsubscribe();
  return serverCleared;
}

// Web Push wants the VAPID public key as a Uint8Array of the URL-safe
// base64 decoded bytes. Standard atob() only handles the regular
// alphabet, so we substitute back to + and / and pad.
function urlBase64ToUint8Array(b64: string): Uint8Array {
  const padding = "=".repeat((4 - (b64.length % 4)) % 4);
  const base64 = (b64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}
