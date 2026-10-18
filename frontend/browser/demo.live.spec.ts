import { test, expect } from "@playwright/test";

test.beforeAll(() => {
  if (process.env.TX_LIVE_DISPOSABLE_DB !== "1" || ![process.env.TX_LIVE_API_URL, process.env.TX_LIVE_FRONTEND_URL].every(origin => origin && ["localhost", "127.0.0.1"].includes(new URL(origin).hostname))) {
    throw new Error("Demo auth acceptance requires explicitly disposable loopback API/database/frontend.");
  }
});

for (const persona of ["customer", "recipient", "merchant", "operator"] as const) {
  test(`real demo ${persona} issues a scoped session and normalizes the route`, async ({ page, request }) => {
    const api = process.env.TX_LIVE_API_URL!;
    const options = await request.get(`${api}/api/auth/options`);
    expect(options.ok()).toBe(true);
    expect((await options.json()).data).toMatchObject({ demoEnabled: true, registrationEnabled: false, googleEnabled: false });
    await page.goto(`/demo?role=${persona}`);
    await page.getByLabel("Appearance").selectOption("dark");
    await page.getByRole("button", { name: persona === "recipient" ? "Open Bob · Bank B" : `Explore ${persona}`, exact: true }).click();
    const expectedRole = persona === "merchant" ? "MERCHANT" : persona === "operator" ? "OPS_VIEWER" : "CUSTOMER";
    await expect(page).toHaveURL(persona === "operator" ? /\/console\/worklist$/ : persona === "merchant" ? /\/merchant$/ : /\/$/);
    const token = await page.evaluate(() => sessionStorage.getItem("transactx.token"));
    expect(token).toBeTruthy();
    const profile = await request.get(`${api}/api/me`, { headers: { Authorization: `Bearer ${token}` } });
    expect(profile.ok()).toBe(true);
    expect((await profile.json()).data.role).toBe(expectedRole);
    await expect(page.getByRole("heading", { name: persona === "operator" ? "Recovery worklist" : persona === "merchant" ? /^Welcome,/ : /Good day/ })).toBeVisible();
    const forbidden = await request.post(`${api}/api/ops/chaos/start`, { headers: { Authorization: `Bearer ${token}` }, data: {} });
    expect(forbidden.status()).toBe(403);
    await page.getByRole("button", { name: "Log out", exact: true }).first().click();
    await expect(page).toHaveURL(/\/login$/);
    expect(await page.evaluate(() => sessionStorage.getItem("transactx.token"))).toBeNull();
    await expect(page.getByLabel("Appearance")).toHaveValue("dark");
  });
}

test("real demo endpoint refuses caller-supplied roles and keeps registration closed", async ({ request }) => {
  const api = process.env.TX_LIVE_API_URL!;
  for (const data of [{ persona: "operator", role: "OPS_ADMIN" }, { persona: "customer", userId: "arbitrary" }]) {
    const response = await request.post(`${api}/api/auth/demo`, { data });
    expect(response.status()).toBe(400);
  }
  const admin = await request.post(`${api}/api/auth/demo`, { data: { persona: "admin" } });
  expect(admin.status()).toBe(503);
  const registration = await request.post(`${api}/api/auth/register`, { data: {} });
  expect(registration.status()).toBe(403);
});
