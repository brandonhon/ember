import { test, expect } from "@playwright/test";

test.describe("authentication", () => {
  test("anonymous user lands on the login screen", async ({ page }) => {
    await page.goto("/");
    // Login form has an h1 of "Ember" and a username field with data-testid.
    await expect(page.getByTestId("username")).toBeVisible();
    await expect(page.getByTestId("password")).toBeVisible();
    await expect(page.getByTestId("login-submit")).toBeVisible();
  });

  test("bad credentials are rejected with a visible error", async ({ page }) => {
    await page.goto("/");
    await page.getByTestId("username").fill("admin");
    await page.getByTestId("password").fill("definitely-wrong");
    await page.getByTestId("login-submit").click();
    await expect(page.getByTestId("login-error")).toBeVisible();
    // Still on the login screen.
    await expect(page.getByTestId("username")).toBeVisible();
  });

  test("valid credentials log in and show the three-pane shell", async ({ page }) => {
    await page.goto("/");
    await page.getByTestId("username").fill("admin");
    await page.getByTestId("password").fill("admintest");
    await page.getByTestId("login-submit").click();

    // The shell renders the article list element.
    await expect(page.getByTestId("article-list")).toBeVisible();
    // Sidebar has the seeded feed.
    await expect(page.getByTestId("feed-1")).toBeVisible();
  });

  test("logout returns to the login screen", async ({ page }) => {
    await page.goto("/");
    await page.getByTestId("username").fill("admin");
    await page.getByTestId("password").fill("admintest");
    await page.getByTestId("login-submit").click();
    await expect(page.getByTestId("article-list")).toBeVisible();

    // Logout is in the user popover.
    await page.locator('[data-user-chip]').click();
    await page.getByTestId("logout").click();
    await expect(page.getByTestId("username")).toBeVisible();
  });
});
