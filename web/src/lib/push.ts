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
  return res.data?.id ?? 0;
}

export async function disablePush(): Promise<void> {
  const reg = await navigator.serviceWorker.getRegistration();
  if (!reg) return;
  const sub = await reg.pushManager.getSubscription();
  if (!sub) return;
  try {
    // The server's by-endpoint cleanup endpoint isn't exposed (we don't
    // want a public endpoint that lets anyone purge a subscription by
    // guessing endpoints). Use the list-then-delete-by-id path: list our
    // subs, find the one with our endpoint, delete it.
    const list = await api.pushSubscriptions();
    // We don't expose endpoint on list responses for security — so
    // instead, just unsubscribe locally and let the server clean up on
    // the next 410 / on user logout. This means the row may linger
    // server-side until next-push-fails, which is fine.
    void list;
  } catch (err) {
    if (!(err instanceof ApiError)) throw err;
  }
  await sub.unsubscribe();
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
