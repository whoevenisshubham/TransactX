import { expect, test } from "@playwright/test";
import type { AnalyticsWindow } from "../src/ops/contracts";

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
  const downloading = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download server export" }).click();
  const download = await downloading;
  expect(download.suggestedFilename()).toBe(`transactx-evidence-${paymentId}.json`);
  const stream = await download.createReadStream();
  expect(stream).not.toBeNull();
  let contents = "";
  for await (const chunk of stream!) contents += chunk.toString();
  const exported = JSON.parse(contents);
  expect(exported.bundle.schemaVersion).toBe("tx.evidence.v1");
  expect(exported.manifest.paymentId).toBe(paymentId);
  expect(exported.manifest.snapshotId).toBe(exported.bundle.snapshotId);
  expect(exported.manifest.algorithm).toBe("SHA-256");
  expect(exported.manifest.bundleSha256).toMatch(/^[a-f0-9]{64}$/);
  expect(contents).not.toContain(password);
  expect(contents).not.toMatch(/password_hash|JWT_SECRET|BANK_A_SERVICE_TOKEN|authorization/i);
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
  const citations = page.getByRole("link", { name: /^View evidence / });
  await expect(citations.first()).toBeVisible();
  const references = [...new Set(await citations.evaluateAll((links) => links.map((link) => (link as HTMLAnchorElement).href)))];
  await citations.first().focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  for (const reference of references) {
    const url = new URL(reference);
    expect(url.pathname).toBe(`/console/payments/${paymentId}`);
    const evidenceElement = decodeURIComponent(url.hash.slice(1));
    expect(evidenceElement.startsWith("evidence-")).toBe(true);
    await page.goto(reference);
    await expect(page.locator(`[id=${JSON.stringify(evidenceElement)}]`)).toBeVisible();
  }
});

for (const [role, identity] of Object.entries(identities)) {
  test(`${role} opens a real worklist payment and its evidence`, async ({ page, context }) => {
    test.setTimeout(60000);
    let authoritativeReads = 0;
    page.on("response", (response) => {
      if (new URL(response.url()).pathname === "/api/ops/worklist" && response.status() === 200) authoritativeReads += 1;
    });
    await page.setViewportSize({ width: 360, height: 800 });
    await page.goto(`/console/worklist?state=${encodeURIComponent(state)}&paymentId=${encodeURIComponent(paymentId)}`);
    await page.getByLabel("Phone or payment ID").fill(identity!);
    await page.getByLabel("Password").fill(password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();
    if (role === "viewer") {
      await expect(page.getByText("LIVE HINTS CONNECTED", { exact: true })).toBeVisible();
      const beforeGap = authoritativeReads;
      await context.setOffline(true);
      await expect(page.getByText("LIVE HINTS DISCONNECTED", { exact: true })).toBeVisible({ timeout: 40000 });
      await context.setOffline(false);
      await expect(page.getByText("LIVE HINTS CONNECTED", { exact: true })).toBeVisible();
      await expect.poll(() => authoritativeReads).toBeGreaterThan(beforeGap);
    }
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

test("an expired real session is cleared after reconnect", async ({ page, context }) => {
  test.skip(process.env.TX_LIVE_EXPECT_EXPIRY !== "1", "Requires a disposable API with JWT_LIFETIME of at most 10 seconds.");
  await page.goto(`/console/payments/${paymentId}`);
  await page.getByLabel("Phone or payment ID").fill(identities.viewer!);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  const expiration = await page.evaluate(() => {
    const token = sessionStorage.getItem("transactx.token")!;
    const payload = token.split(".")[1].replaceAll("-", "+").replaceAll("_", "/");
    return JSON.parse(atob(payload)).exp as number;
  });
  expect(expiration * 1000 - Date.now()).toBeLessThanOrEqual(10000);
  await context.setOffline(true);
  await expect.poll(() => Math.floor(Date.now() / 1000) > expiration, { timeout: 12000 }).toBe(true);
  await context.setOffline(false);
  await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Payment journey" })).toHaveCount(0);
  expect(await page.evaluate(() => sessionStorage.getItem("transactx.token"))).toBeNull();
  expect(await page.evaluate(() => sessionStorage.getItem("transactx.profile"))).toBeNull();
});

test("viewer renders durable analytics and cannot invoke admin controls", async ({ page, request }) => {
  const api = new URL(process.env.TX_LIVE_API_URL ?? "http://invalid.invalid");
  if (process.env.TX_LIVE_DISPOSABLE_DB !== "1" || !["localhost", "127.0.0.1"].includes(api.hostname)) {
    throw new Error("Analytics/role live check requires an explicitly disposable loopback API with measured windows and control routes enabled.");
  }
  await page.setViewportSize({ width: 360, height: 800 });
  const windowsReply = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/ops/analytics/windows" && response.status() === 200);
  await page.goto("/console/analytics");
  await page.getByLabel("Phone or payment ID").fill(identities.viewer!);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  const { data }: { data: { items: AnalyticsWindow[] } } = await (await windowsReply).json();
  expect(data.items.length).toBeGreaterThan(0);
  await expect(page.getByRole("heading", { name: "Operational analytics" })).toBeVisible();
  await expect(page.getByText("These measurements never authorize routing or settlement.", { exact: false })).toBeVisible();
  const table = page.getByRole("table", { name: "Measured operational windows with a table alternative to charts" });
  await expect(table).toBeVisible();
  await expect(table.locator("tbody tr")).toHaveCount(data.items.length);
  const nullableIndex = data.items.findIndex((item) => item.features.p95LatencyMs === null);
  expect(nullableIndex, "durable windows must include an unavailable latency measurement").toBeGreaterThanOrEqual(0);
  await expect(table.locator("tbody tr").nth(nullableIndex).getByRole("cell").nth(4)).toHaveText("Unavailable");
  const modelIndex = data.items.findIndex((item) => item.model.score === null && item.model.status !== "ready");
  expect(modelIndex, "model-off/insufficient status must be represented").toBeGreaterThanOrEqual(0);
  await expect(table.locator("tbody tr").nth(modelIndex).getByRole("cell").nth(5)).toContainText(data.items[modelIndex].model.status.replaceAll("_", " "));
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const token = await page.evaluate(() => sessionStorage.getItem("transactx.token"));
  expect(token).toBeTruthy();
  const deniedRecheck = await request.post(`${api.origin}/api/ops/payments/${paymentId}/recovery/recheck`, {
    headers: { Authorization: `Bearer ${token}`, "Idempotency-Key": crypto.randomUUID() }, data: { reason: "Viewer denial verification" },
  });
  expect(deniedRecheck.status()).toBe(403);
  const deniedChaos = await request.post(`${api.origin}/api/ops/chaos/reset`, {
    headers: { Authorization: `Bearer ${token}` }, data: {},
  });
  expect(deniedChaos.status()).toBe(403);
});
