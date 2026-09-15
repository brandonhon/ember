import { describe, it, expect, beforeEach, vi } from "vitest";
import { disablePush, storedPushSubID, forgetPushSubID } from "./push";
import { ApiError } from "./api";

const SUB_ID_KEY = "ember:push-sub-id";

const unsubscribe = vi.fn(async () => true);
const getSubscription = vi.fn(async () => ({ unsubscribe }) as unknown);
const getRegistration = vi.fn(async () => ({ pushManager: { getSubscription } }) as unknown);
const pushUnsubscribe = vi.fn(async (_id: number) => ({ data: { ok: true } }));

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    api: { pushUnsubscribe: (id: number) => pushUnsubscribe(id) },
  };
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  Object.defineProperty(globalThis, "navigator", {
    value: { serviceWorker: { getRegistration } },
    configurable: true,
    writable: true,
  });
});

describe("disablePush", () => {
  it("deletes the remembered server row, forgets it, and unsubscribes the browser", async () => {
    localStorage.setItem(SUB_ID_KEY, "42");
    const cleared = await disablePush();

    expect(pushUnsubscribe).toHaveBeenCalledWith(42);
    expect(cleared).toBe(true);
    expect(storedPushSubID()).toBe(0); // forgotten
    expect(unsubscribe).toHaveBeenCalledTimes(1); // browser no longer subscribed
  });

  it("still unsubscribes the browser when no id was remembered", async () => {
    // Enabled before the id was recorded, or storage was cleared. The device
    // must still stop being subscribed; the caller is told the row survived.
    const cleared = await disablePush();

    expect(pushUnsubscribe).not.toHaveBeenCalled();
    expect(cleared).toBe(false);
    expect(unsubscribe).toHaveBeenCalledTimes(1);
  });

  it("treats a 404 from the server as already gone", async () => {
    localStorage.setItem(SUB_ID_KEY, "7");
    pushUnsubscribe.mockRejectedValueOnce(new ApiError(404, "not_found", "gone"));

    await expect(disablePush()).resolves.toBe(true);
    expect(unsubscribe).toHaveBeenCalledTimes(1);
  });

  it("reports failure but still unsubscribes when the server rejects", async () => {
    localStorage.setItem(SUB_ID_KEY, "7");
    pushUnsubscribe.mockRejectedValueOnce(new ApiError(500, "internal", "boom"));

    // The local unsubscribe is the part the user asked for — it must happen
    // even though the row could not be removed.
    await expect(disablePush()).resolves.toBe(false);
    expect(unsubscribe).toHaveBeenCalledTimes(1);
    expect(storedPushSubID()).toBe(0);
  });

  it("is a no-op on the browser side when nothing is subscribed", async () => {
    getSubscription.mockResolvedValueOnce(null);
    await expect(disablePush()).resolves.toBe(false);
    expect(unsubscribe).not.toHaveBeenCalled();
  });
});

describe("push subscription id storage", () => {
  it("round-trips and clears", () => {
    localStorage.setItem(SUB_ID_KEY, "13");
    expect(storedPushSubID()).toBe(13);
    forgetPushSubID();
    expect(storedPushSubID()).toBe(0);
  });

  it("returns 0 for junk rather than NaN", () => {
    localStorage.setItem(SUB_ID_KEY, "not-a-number");
    expect(storedPushSubID()).toBe(0);
  });
});
