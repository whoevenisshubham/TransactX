import { expect, test, type APIRequestContext, type Page, type Route } from "@playwright/test";

type Payment = { id: string; note?: string; state: string; direction: string };
type Envelope<T> = { data: T };

const apiOrigin = process.env.TX_LIVE_API_URL;
const sender = process.env.TX_LIVE_SENDER_ID;
const receiver = process.env.TX_LIVE_RECEIVER_ID;
const other = process.env.TX_LIVE_OTHER_ID;
const password = process.env.TX_LIVE_CUSTOMER_PASSWORD;

function requireDisposableStack() {
  const frontend = new URL(process.env.TX_LIVE_FRONTEND_URL ?? "http://127.0.0.1:4174");
  const api = new URL(apiOrigin ?? "http://invalid.invalid");
  if (!(["http:", "https:"].includes(frontend.protocol)) || api.protocol !== "https:" ||
      ![frontend, api].every((url) => ["127.0.0.1", "localhost"].includes(url.hostname))) {
    throw new Error("Offline live payment check requires a loopback frontend and HTTPS loopback API in a production frontend build.");
  }
  if (process.env.TX_LIVE_DISPOSABLE_DB !== "1" || !sender || !receiver || !other || !password || !apiOrigin) {
    throw new Error("Set TX_LIVE_DISPOSABLE_DB=1 and the disposable API, sender, receiver, other-user and password inputs.");
  }
  expect(new Set([sender, receiver, other]).size).toBe(3);
  return api.origin;
}

async function tokenFor(request: APIRequestContext, origin: string, identifier: string): Promise<string> {
  const response = await request.post(`${origin}/api/auth/login`, { data: { identifier, password } });
  expect(response.ok(), `login for ${identifier}`).toBe(true);
  return ((await response.json()) as Envelope<{ token: string }>).data.token;
}

async function paymentsFor(request: APIRequestContext, origin: string, token: string): Promise<Payment[]> {
  const response = await request.get(`${origin}/api/payments?limit=50`, { headers: { Authorization: `Bearer ${token}` } });
  expect(response.ok()).toBe(true);
  return ((await response.json()) as Envelope<Payment[]>).data;
}

async function signIn(page: Page, identifier: string) {
  await page.getByLabel("Phone or payment ID").fill(identifier);
  await page.getByLabel("Password").fill(password!);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("button", { name: "Log out of TransactX" })).toBeVisible();
}

test("a retained intent survives a full offline reload and another user cannot replay it", async ({ page, context, request }) => {
  test.setTimeout(100_000);
  const origin = requireDisposableStack();
  const senderToken = await tokenFor(request, origin, sender!);
  const otherToken = await tokenFor(request, origin, other!);
  const note = `m1-offline-${crypto.randomUUID()}`;
  const matching = async (token: string) => (await paymentsFor(request, origin, token)).filter((payment) => payment.note === note);
  const attemptedKeys: string[] = [];
  const attemptedRequestIds: string[] = [];
  let outage = false;
  const routeApi = async (route: Route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) return route.abort("failed");
    if (url.pathname !== "/api/payments" || route.request().method() !== "POST") return route.continue();
    attemptedKeys.push(route.request().headers()["idempotency-key"]);
    attemptedRequestIds.push(route.request().headers()["x-request-id"]);
    if (outage) await route.abort("failed");
    else await route.continue();
  };
  await page.route("**/api/**", routeApi);

  await page.goto("/");
  await signIn(page, sender!);
  await page.getByRole("button", { name: "Pay", exact: true }).click();
  await page.getByLabel("Recipient TransactX payment ID").fill(receiver!);
  await page.getByRole("button", { name: "Find", exact: true }).click();
  await expect(page.getByText(receiver!, { exact: true }).first()).toBeVisible();
  await page.getByLabel("Amount in Indian rupees").fill("1.00");
  await page.getByLabel("Optional note").fill(note);
  await page.getByRole("button", { name: "Review payment" }).click();

  await expect.poll(() => page.evaluate(() => Boolean(navigator.serviceWorker.controller)), { timeout: 15000 }).toBe(true);
  outage = true;
  await page.getByRole("button", { name: "Confirm payment" }).click();
  await expect(page.getByRole("heading", { name: "Payment queued for replay" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Offline payment intents" })).toBeVisible();
  expect(await matching(senderToken)).toHaveLength(0);

  await page.unroute("**/api/**", routeApi);
  await context.setOffline(true);
  await page.reload();
  await expect(page.getByRole("heading", { name: "You're offline" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Offline payment intents" })).toBeVisible();
  await expect(page.getByText(receiver!, { exact: false })).toBeVisible();
  await page.getByRole("button", { name: "Log out" }).click();
  outage = false;
  await page.route("**/api/**", routeApi);
  await context.setOffline(false);
  await signIn(page, other!);
  await expect(page.getByRole("heading", { name: "Offline payment intents" })).toHaveCount(0);

  // A new session and reload run the other user's replay worker, which must
  // never claim the sender's IndexedDB record.
  await page.reload();
  await expect(page.getByRole("heading", { name: "Offline payment intents" })).toHaveCount(0);
  expect(await matching(senderToken)).toHaveLength(0);
  expect(await matching(otherToken)).toHaveLength(0);

  await page.getByRole("button", { name: "Log out of TransactX" }).click();
  await signIn(page, sender!);
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect.poll(async () => (await matching(senderToken)).length, { timeout: 30_000 }).toBe(1);
  const [payment] = await matching(senderToken);
  expect(payment.direction).toBe("SENT");
  await expect.poll(async () => (await matching(senderToken))[0]?.state, { timeout: 30_000 }).toBe("COMPLETED");
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await page.reload();
  expect(await matching(senderToken)).toHaveLength(1);
  expect(await matching(otherToken)).toHaveLength(0);
  const hidden = await request.get(`${origin}/api/payments/${payment.id}`, { headers: { Authorization: `Bearer ${otherToken}` } });
  expect(hidden.status()).toBe(404);
  expect(attemptedKeys.length).toBeGreaterThanOrEqual(2);
  expect(new Set(attemptedKeys).size).toBe(1);
  expect(new Set(attemptedRequestIds).size).toBe(1);
});
