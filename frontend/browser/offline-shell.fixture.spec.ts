import { expect, test } from "@playwright/test";

test("production shell reloads fully offline and retains the owner's saved intent", async ({ page, context }) => {
  await page.addInitScript(() => {
    sessionStorage.setItem("transactx.token", "fixture-token");
    const realFetch = window.fetch.bind(window);
    window.fetch = (input, init) => {
      const url = new URL(input instanceof Request ? input.url : String(input), location.href);
      if (url.origin !== "https://127.0.0.1:18080" || !url.pathname.startsWith("/api/")) return realFetch(input, init);
      if (!navigator.onLine || (url.pathname === "/api/payments" && init?.method === "POST")) return Promise.reject(new TypeError("Network unavailable"));
      const data = url.pathname === "/api/me"
        ? { id: "offline-owner", name: "Offline Owner", role: "CUSTOMER", paymentIdentifier: "owner@fixture" }
        : url.pathname === "/api/accounts"
          ? [{ accountNumber: "A-1", balancePaise: 10000, status: "ACTIVE" }]
          : url.pathname === "/api/payments"
            ? []
            : url.pathname.startsWith("/api/recipients/")
              ? { name: "Receiver", paymentIdentifier: "receiver@fixture", status: "ACTIVE" }
              : null;
      return Promise.resolve(new Response(JSON.stringify({ requestId: "fixture", data }), { status: data === null ? 404 : 200, headers: { "content-type": "application/json" } }));
    };
  });

  await page.goto("/");
  await expect(page.getByRole("button", { name: "Pay", exact: true })).toBeVisible();
  await expect.poll(() => page.evaluate(() => Boolean(navigator.serviceWorker.controller)), { timeout: 15000 }).toBe(true);
  const cached = await page.evaluate(async () => {
    const names = (await caches.keys()).filter(name => name.startsWith("transactx-static-shell-"));
    return (await Promise.all(names.map(async name => (await (await caches.open(name)).keys()).map(request => new URL(request.url).pathname)))).flat();
  });
  for (const asset of ["CustomerShell-", "MerchantShell-", "console-", ".woff2"]) expect(cached.some(path => path.includes(asset))).toBe(true);
  expect(cached.some(path => path.startsWith("/api/") || path.startsWith("/auth/"))).toBe(false);
  await page.getByRole("button", { name: "Pay", exact: true }).click();
  await page.getByLabel("Recipient TransactX payment ID").fill("receiver@fixture");
  await page.getByRole("button", { name: "Find", exact: true }).click();
  await expect(page.getByText("receiver@fixture", { exact: true }).first()).toBeVisible();
  await page.getByLabel("Amount in Indian rupees").fill("1.00");
  await page.getByRole("button", { name: "Review payment" }).click();
  await page.getByRole("button", { name: "Confirm payment" }).click();
  await expect(page.getByRole("heading", { name: "Payment queued for replay" })).toBeVisible();

  await context.setOffline(true);
  await page.reload();
  expect(await page.evaluate(() => navigator.onLine)).toBe(false);
  await expect(page.getByRole("heading", { name: "You're offline" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Offline payment intents" })).toBeVisible();
  await expect(page.getByText("receiver@fixture", { exact: false })).toBeVisible();
  await expect(page.getByText("Payment status and account details cannot be verified", { exact: false })).toBeVisible();
  await page.getByRole("button", { name: "Log out" }).click();
  await expect(page.getByRole("heading", { name: "Sign in to continue" })).toBeVisible();
  expect(await page.evaluate(() => [sessionStorage.getItem("transactx.token"), sessionStorage.getItem("transactx.profile")])).toEqual([null, null]);
});

test("an expired token clears the cached offline owner", async ({ page }) => {
  await page.addInitScript(() => {
    sessionStorage.setItem("transactx.token", "expired-fixture-token");
    sessionStorage.setItem("transactx.profile", JSON.stringify({ token: "expired-fixture-token", profile: { id: "previous-owner", role: "CUSTOMER" } }));
    const realFetch = window.fetch.bind(window);
    window.fetch = (input, init) => {
      const url = new URL(input instanceof Request ? input.url : String(input), location.href);
      if (url.pathname === "/api/me") {
        return Promise.resolve(new Response(JSON.stringify({ error: { code: "UNAUTHORIZED" } }), { status: 401, headers: { "content-type": "application/json" } }));
      }
      return realFetch(input, init);
    };
  });
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Sign in to continue" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "You're offline" })).toHaveCount(0);
  expect(await page.evaluate(() => [sessionStorage.getItem("transactx.token"), sessionStorage.getItem("transactx.profile")])).toEqual([null, null]);
});
