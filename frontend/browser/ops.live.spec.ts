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
