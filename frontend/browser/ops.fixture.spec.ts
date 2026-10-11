import { readFileSync } from "node:fs";
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

const paymentId = "11111111-1111-4111-8111-111111111111";
const investigationId = "44444444-4444-4444-8444-444444444444";
function fixture(name: string) {
  return JSON.parse(readFileSync(new URL(`../../docs/next-gen/execution-pack/docs/next-gen/contracts/fixtures/${name}`, import.meta.url), "utf8"));
}

function worklistFixture() {
  const response = fixture("worklist-empty.json");
  response.data.items = [{
    paymentId, state: "PENDING_RECONCILIATION", amountPaise: "12500", currency: "INR",
    sourceBankCode: "BANK-A", destinationBankCode: "BANK-B",
    createdAt: "2026-10-11T00:00:00Z", updatedAt: "2026-10-11T00:00:00Z", ageMs: 60000,
    recoveryStatus: "deferred", attemptCount: 2, nextAttemptAt: null,
    reasonCode: "PARTICIPANT_UNAVAILABLE", projectionVersion: "5",
  }];
  return response;
}

async function installFixtureApi(page: Page, role: "OPS_VIEWER" | "OPS_ADMIN", onRecheck?: (key: string, attempt: number) => boolean, onInvestigation?: (key: string, attempt: number) => boolean) {
  await page.addInitScript(() => sessionStorage.setItem("transactx.token", "browser-fixture-token"));
  let recheckAttempts = 0;
  let investigationAttempts = 0;
  await page.route("http://localhost:8080/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const headers = {
      "access-control-allow-origin": "http://127.0.0.1:4173",
      "access-control-allow-methods": "GET, POST, OPTIONS",
      "access-control-allow-headers": "Authorization, Content-Type, Idempotency-Key",
    };
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    let status = 200;
    let data: unknown;
    if (path === "/api/me") data = { id: "user-fixture", name: "Operator Fixture", paymentIdentifier: "operator@fixture", role };
    else if (path === "/api/ops/runtime") data = { simulationMode: true };
    else if (path === "/api/ops/worklist") data = worklistFixture().data;
    else if (path === `/api/ops/payments/${paymentId}/evidence`) data = fixture("evidence-partial.json").data;
    else if (path === `/api/ops/payments/${paymentId}/recovery-attempts`) data = { items: [], nextCursor: null, asOf: "2026-10-11T00:00:00Z" };
    else if (path === `/api/ops/payments/${paymentId}/evidence/export`) data = { manifest: { fixture: true }, evidence: fixture("evidence-partial.json").data };
    else if (path === `/api/ops/payments/${paymentId}/recovery/recheck` && request.method() === "POST") {
      recheckAttempts += 1;
      if (onRecheck?.(request.headers()["idempotency-key"], recheckAttempts)) return route.abort("failed");
      status = 202;
      data = { paymentId, scheduledAt: "2026-10-11T00:00:00Z", status: "scheduled" };
    } else if (path === "/api/ops/investigations" && request.method() === "POST") {
      investigationAttempts += 1;
      if (onInvestigation?.(request.headers()["idempotency-key"], investigationAttempts)) return route.abort("failed");
      status = 202;
      data = { investigationId, status: "queued", mode: "evidence_only" };
    } else if (path === `/api/ops/investigations/${investigationId}`) data = fixture("investigation-evidence-only.json").data;
    else if (path === "/api/ops/events/stream") return route.fulfill({ status: 503, headers, body: "" });
    else return route.fulfill({ status: 404, headers, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", error: { code: "NOT_FOUND", message: "Fixture endpoint missing" } }) });
    return route.fulfill({ status, headers, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", data }) });
  });
}

test("viewer can refresh a deep-linked worklist and inspect partial payment evidence", async ({ page }) => {
  await installFixtureApi(page, "OPS_VIEWER");
  await page.goto("/console/worklist?state=PENDING_RECONCILIATION");
  await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();
  await expect(page.getByRole("link", { name: paymentId })).toBeVisible();
  await page.getByRole("link", { name: paymentId }).click();
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  await expect(page.getByText("An unavailable or unknown bank observation does not prove failure.", { exact: false })).toBeVisible();
  await expect(page.getByRole("button", { name: "Schedule recheck" })).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  await page.getByRole("button", { name: "Back to worklist" }).click();
  await expect(page).toHaveURL(/\/console\/worklist\?state=PENDING_RECONCILIATION/);
});

test("admin retry reuses the idempotency key after an ambiguous network failure", async ({ page }) => {
  const keys: string[] = [];
  await installFixtureApi(page, "OPS_ADMIN", (key, attempt) => { keys.push(key); return attempt === 1; });
  await page.goto(`/console/payments/${paymentId}`);
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  await page.getByLabel("Reason").fill("Operator review requested");
  await page.getByRole("button", { name: "Schedule recheck" }).click();
  await expect(page.getByRole("alert")).toContainText("couldn't reach TransactX");
  await page.getByRole("button", { name: "Schedule recheck" }).click();
  await expect(page.getByText("Recheck scheduled", { exact: false })).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
});

test("evidence-only investigation citations resolve and new pages have no serious axe findings", async ({ page }) => {
  await installFixtureApi(page, "OPS_VIEWER");
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto(`/console/payments/${paymentId}`);
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  await page.getByLabel("Question").fill("Why is the payment pending?");
  await page.getByRole("button", { name: "Start investigation" }).click();
  await expect(page.getByText("Evidence summary", { exact: false })).toBeVisible();
  await expect(page.getByText("The central payment is PENDING_RECONCILIATION.")).toBeVisible();
  await page.getByRole("button", { name: /View evidence payment:/ }).click();
  const scan = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  const serious = scan.violations.filter((violation) => violation.impact === "critical" || violation.impact === "serious");
  expect(serious.map((violation) => ({ id: violation.id, nodes: violation.nodes.map((node) => ({ target: node.target, summary: node.failureSummary })) }))).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
});

test("investigation retry keeps its key and payment evidence available", async ({ page }) => {
  const keys: string[] = [];
  await installFixtureApi(page, "OPS_VIEWER", undefined, (key, attempt) => { keys.push(key); return attempt === 1; });
  await page.goto(`/console/payments/${paymentId}`);
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  await page.getByLabel("Question").fill("What is known about the pending state?");
  await page.getByRole("button", { name: "Start investigation" }).click();
  await expect(page.getByRole("alert")).toContainText("couldn't reach TransactX");
  await expect(page.getByText("An unavailable or unknown bank observation does not prove failure.", { exact: false })).toBeVisible();
  await page.getByRole("button", { name: "Start investigation" }).click();
  await expect(page.getByText("The central payment is PENDING_RECONCILIATION.")).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
});
