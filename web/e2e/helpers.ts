import { type Page, expect } from "@playwright/test";

export const TEST_USER = "admin";
export const TEST_PASSWORD = "admintest";

// signIn navigates to / and logs in via the form. Asserts the shell becomes
// visible.
export async function signIn(page: Page): Promise<void> {
  await page.goto("/");
  await page.getByTestId("username").fill(TEST_USER);
  await page.getByTestId("password").fill(TEST_PASSWORD);
  await page.getByTestId("login-submit").click();
  await expect(page.getByTestId("article-list")).toBeVisible();
}
