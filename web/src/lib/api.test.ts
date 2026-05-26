import { describe, it, expect, beforeEach, vi } from "vitest";
import { api, ApiError } from "./api";

type FetchInit = RequestInit;

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  global.fetch = fetchMock;
});

function ok<T>(data: T, meta?: Record<string, unknown>) {
  return new Response(JSON.stringify({ data, meta }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function err(status: number, code: string, message: string) {
  return new Response(JSON.stringify({ error: { code, message } }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("api client", () => {
  it("builds GET URLs with query params", async () => {
    fetchMock.mockResolvedValueOnce(ok([], { next_cursor_id: 0 }));
    await api.listArticles({ view: "fresh", feed_id: 5, limit: 25 });
    const [url, init] = fetchMock.mock.calls[0] as [string, FetchInit];
    expect(url).toContain("/api/articles?");
    expect(url).toContain("view=fresh");
    expect(url).toContain("feed_id=5");
    expect(url).toContain("limit=25");
    expect(init.method).toBe("GET");
    expect(init.credentials).toBe("include");
  });

  it("omits falsy / undefined params from query string", async () => {
    fetchMock.mockResolvedValueOnce(ok([]));
    await api.listArticles({
      view: "",
      feed_id: undefined,
      unread: false,
      limit: 30,
    });
    const [url] = fetchMock.mock.calls[0] as [string, unknown];
    expect(url).not.toContain("view=");
    expect(url).not.toContain("feed_id=");
    expect(url).not.toContain("unread=");
    expect(url).toContain("limit=30");
  });

  it("POSTs JSON bodies", async () => {
    fetchMock.mockResolvedValueOnce(ok({ id: 1 }));
    await api.createCategory({ name: "Tech" });
    const [url, init] = fetchMock.mock.calls[0] as [string, FetchInit];
    expect(url).toBe("/api/categories");
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe(
      "application/json",
    );
    expect(init.body).toBe(JSON.stringify({ name: "Tech" }));
  });

  it("throws ApiError on JSON error envelopes", async () => {
    fetchMock.mockResolvedValueOnce(err(404, "not_found", "missing"));
    try {
      await api.getArticle(1);
      throw new Error("should have thrown");
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError);
      const a = e as ApiError;
      expect(a.status).toBe(404);
      expect(a.code).toBe("not_found");
    }
  });

  it("dispatches ember:unauthorized event on 401 (except login)", async () => {
    const handler = vi.fn();
    window.addEventListener("ember:unauthorized", handler);
    fetchMock.mockResolvedValueOnce(err(401, "unauthorized", "go away"));
    await expect(api.me()).rejects.toThrow(ApiError);
    expect(handler).toHaveBeenCalledTimes(1);
    window.removeEventListener("ember:unauthorized", handler);
  });

  it("does NOT dispatch ember:unauthorized on login 401", async () => {
    const handler = vi.fn();
    window.addEventListener("ember:unauthorized", handler);
    fetchMock.mockResolvedValueOnce(err(401, "invalid_credentials", "nope"));
    await expect(api.login("u", "p")).rejects.toThrow(ApiError);
    expect(handler).not.toHaveBeenCalled();
    window.removeEventListener("ember:unauthorized", handler);
  });

  it("handles non-JSON error body gracefully", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response("oops", { status: 500, statusText: "Internal Server Error" }),
    );
    try {
      await api.me();
      throw new Error("should have thrown");
    } catch (e) {
      const a = e as ApiError;
      expect(a.status).toBe(500);
      expect(a.code).toBe("http_500");
    }
  });
});
