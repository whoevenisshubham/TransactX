import { expect, test } from "@playwright/test";

const paymentId = process.env.TX_LIVE_PAYMENT_ID;
const state = process.env.TX_LIVE_PAYMENT_STATE;
const password = process.env.TX_LIVE_PASSWORD;
const identities = {
  viewer: process.env.TX_LIVE_VIEWER_ID,
  admin: process.env.TX_LIVE_ADMIN_ID,
};

if (!paymentId || !state || !password || !identities.viewer || !identities.admin) {
  throw new Error("Live browser checks require TX_LIVE_PAYMENT_ID, TX_LIVE_PAYMENT_STATE, TX_LIVE_PASSWORD, TX_LIVE_VIEWER_ID and TX_LIVE_ADMIN_ID.");
}

test("viewer creates a real evidence-only investigation completed by the native worker", async ({ page }) => {
  test.setTimeout(90000);
  const frontend = new URL(process.env.TX_LIVE_FRONTEND_URL ?? "http://127.0.0.1:4174");
  const api = new URL(process.env.TX_LIVE_API_URL ?? "http://invalid.invalid");
  if (process.env.TX_LIVE_DISPOSABLE_DB !== "1" ||
      ![frontend, api].every((url) => ["http:", "https:"].includes(url.protocol) && ["localhost", "127.0.0.1"].includes(url.hostname))) {
    throw new Error("Investigation live check requires TX_LIVE_DISPOSABLE_DB=1 and loopback frontend/API URLs with a native worker and provider calls disabled.");
  }
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto(`/console/payments/${paymentId}`);
  await page.getByLabel("Phone or payment ID").fill(identities.viewer!);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  await page.getByLabel("Question", { exact: true }).fill("What collected evidence explains this payment's current state?");
  const accepted = page.waitForResponse((response) => response.url() === `${api.origin}/api/ops/investigations` && response.request().method() === "POST");
  await page.getByRole("button", { name: "Start investigation", exact: true }).click();
  const response = await accepted;
  expect(response.status()).toBe(202);
  expect(response.request().headers()["idempotency-key"]).toBeTruthy();
  const { data } = await response.json();
  expect(data.mode).toBe("evidence_only");
  expect(data.investigationId).toMatch(/^[0-9a-f-]{36}$/i);
  await expect(page.getByText("Status: completed · Mode: Evidence summary", { exact: true })).toBeVisible({ timeout: 60000 });
  await page.getByRole("button", { name: "Open saved investigation" }).click();
  await expect(page).toHaveURL(new RegExp(`/console/investigations/${data.investigationId}\\?paymentId=${paymentId}`));
  await page.reload();
  await expect(page.getByText("Status: completed · Mode: Evidence summary", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Facts", exact: true })).toBeVisible();
  await expect(page.getByText("Coverage:", { exact: false })).toBeVisible();
  await expect(page.getByText("LLM investigation", { exact: false })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
});

for (const [role, identity] of Object.entries(identities)) {
  test(`${role} opens a real worklist payment and its evidence`, async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 800 });
    await page.goto(`/console/worklist?state=${encodeURIComponent(state)}&paymentId=${encodeURIComponent(paymentId)}`);
    await page.getByLabel("Phone or payment ID").fill(identity!);
    await page.getByLabel("Password").fill(password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();
    await page.getByRole("link", { name: paymentId }).click();
    await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
    await expect(page.getByText(`Central payment`, { exact: false }).first()).toBeVisible();
    if (role === "admin") await expect(page.getByRole("button", { name: "Schedule recheck" })).toBeVisible();
    else await expect(page.getByRole("button", { name: "Schedule recheck" })).toHaveCount(0);
    await page.reload();
    await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  });
}
