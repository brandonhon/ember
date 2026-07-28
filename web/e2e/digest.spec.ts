import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

// Regression test for issue #161: enabling the daily digest always failed with
// 400 "invalid request body". GET /api/me/digest returns models.UserDigest,
// which carries the server-owned user_id and last_sent_at; the SPA posted that
// object straight back; and decodeJSON uses DisallowUnknownFields, so Ember
// rejected a body its own interface had produced.
//
// The Go tests cover the server half. This covers the half they cannot: that
// the SPA's actual save request is one the API accepts. It drives the exact
// flow from the report — toggle on, set hour/minute, Save.
test.describe("daily digest", () => {
  async function openDigest(page: import("@playwright/test").Page) {
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByTestId("settings-digest").click();
    await expect(page.getByTestId("digest-save")).toBeVisible();
  }

  test("enabling the digest saves and survives a reload (#161)", async ({ page }) => {
    await signIn(page);
    await openDigest(page);

    await page.getByTestId("digest-on").click();
    await page.getByTestId("digest-hour").fill("7");
    await page.getByTestId("digest-minute").fill("30");
    await page.getByTestId("digest-save").click();

    // The bug surfaced here as "invalid request body".
    await expect(page.getByTestId("digest-msg")).toBeVisible();
    await expect(page.getByTestId("digest-err")).toHaveCount(0);

    // Saved server-side, not just in local component state.
    await page.reload();
    await openDigest(page);
    await expect(page.getByTestId("digest-hour")).toHaveValue("7");
    await expect(page.getByTestId("digest-minute")).toHaveValue("30");
    await expect(page.getByTestId("digest-on")).toHaveClass(/on/);
  });

  // The save request must not carry server-owned fields at all — the endpoint
  // now tolerates them, but the SPA should not be sending server state back.
  test("the save request sends only client-owned fields", async ({ page }) => {
    await signIn(page);
    await openDigest(page);

    const bodies: string[] = [];
    page.on("request", (req) => {
      if (req.method() === "POST" && req.url().includes("/api/me/digest")) {
        bodies.push(req.postData() ?? "");
      }
    });

    await page.getByTestId("digest-on").click();
    await page.getByTestId("digest-save").click();
    await expect(page.getByTestId("digest-msg")).toBeVisible();

    expect(bodies.length).toBeGreaterThan(0);
    const sent = JSON.parse(bodies[0]);
    expect(Object.keys(sent).sort()).toEqual(
      ["email_override", "enabled", "hour_utc", "minute_utc", "view_kind", "view_value"],
    );
  });
});
