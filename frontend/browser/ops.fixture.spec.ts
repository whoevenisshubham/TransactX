import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import AxeBuilder from "@axe-core/playwright";
import { paymentId, investigationId, fixture, worklistFixture, fulfillFixture, installFixtureApi } from "./support/opsFixtures";

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
  await page.getByRole("link", { name: "Detailed evidence ↓" }).click();
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
  const citation = page.getByRole("button", { name: /Payment record \[1\]/ });
  await citation.focus();
  await expect(citation).toBeFocused();
  await page.keyboard.press("Enter");
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

test("worklist cursor, filters, history and unavailable response stay explicit", async ({ page }) => {
  await installFixtureApi(page, "OPS_VIEWER");
  await page.route("http://localhost:8080/api/ops/worklist**", async (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const params = new URL(route.request().url()).searchParams;
    if (params.get("bankCode") === "ERROR") return route.fulfill({ status: 503, headers: { "access-control-allow-origin": "http://127.0.0.1:4173" }, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", error: { code: "UNAVAILABLE", message: "Projection unavailable" } }) });
    const data = worklistFixture().data;
    if (params.has("cursor") || params.get("state") === "FAILED") data.items = [];
    else data.nextCursor = "page2";
    return fulfillFixture(route, data);
  });
  await page.goto("/console/worklist");
  await expect(page.getByRole("link", { name: paymentId })).toBeVisible();
  await page.getByRole("button", { name: "Next page" }).click();
  await expect(page).toHaveURL(/cursor=page2/);
  await expect(page.getByText("No matching payments")).toBeVisible();
  await page.getByRole("button", { name: "First page" }).click();
  await expect(page.getByRole("link", { name: paymentId })).toBeVisible();
  await page.getByLabel("Payment state").selectOption("FAILED");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page.getByText("No matching payments")).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("link", { name: paymentId })).toBeVisible();
  await page.getByLabel("Bank code").fill("ERROR");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page.getByRole("alert")).toContainText("The request could not be completed.");
});

test("terminal evidence hides pending warning and exports the server bundle", async ({ page }) => {
  await installFixtureApi(page, "OPS_VIEWER");
  await page.route(`http://localhost:8080/api/ops/payments/${paymentId}/evidence`, async (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const data = fixture("evidence-partial.json").data;
    data.payment.state = "COMPLETED";
    data.payment.completedAt = "2026-10-11T00:05:00Z";
    data.operations[0].participantStatus = "FINAL";
    data.operations[0].presence = "present";
    data.operations[0].sourceStatus = "available";
    data.sources[1].status = "available";
    data.sources[1].observedAt = "2026-10-11T00:05:00Z";
    data.sources[1].errorCode = null;
    data.recovery.status = "resolved";
    data.warnings = [];
    data.transitions.push({ id: "33333333-3333-4333-8333-333333333334", sequence: "5", fromState: "PENDING_RECONCILIATION", toState: "COMPLETED", occurredAt: "2026-10-11T00:05:00Z", evidenceId: "transition:33333333-3333-4333-8333-333333333334" });
    return fulfillFixture(route, data);
  });
  await page.goto(`/console/payments/${paymentId}`);
  await expect(page.getByText("Completed", { exact: true })).toBeVisible();
  await expect(page.getByText("An unavailable or unknown bank observation does not prove failure.", { exact: false })).toHaveCount(0);
  await page.getByRole("link", { name: "Detailed evidence ↓" }).click();
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download server export" }).click();
  const download = await downloadPromise;
  const payload = JSON.parse(readFileSync(await download.path(), "utf8"));
  expect(payload.manifest.fixture).toBe(true);
  expect(payload.evidence.paymentId).toBe(paymentId);
});

test("saved report labels stale and missing citations without inventing evidence", async ({ page }) => {
  await installFixtureApi(page, "OPS_VIEWER");
  await page.setViewportSize({ width: 360, height: 800 });
  await page.route(`http://localhost:8080/api/ops/investigations/${investigationId}`, async (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const data = fixture("investigation-evidence-only.json").data;
    data.report.snapshotId = "sha256:older-fixture";
    data.report.facts[0].evidenceIds = ["unknown:fixture"];
    data.report.recommendations = [{ text: "Consult the approved runbook.", runbookChunkIds: ["runbook:00986824035ce0aafb0d3c46c3d699ef"] }];
    return fulfillFixture(route, data);
  });
  await page.goto(`/console/investigations/${investigationId}?paymentId=${paymentId}`);
  await expect(page.getByText("This report uses an older evidence snapshot.", { exact: false })).toBeVisible();
  await expect(page.getByText(/Evidence \[\d+\] · Not in this snapshot/)).toBeVisible();
  await expect(page.getByText("Runbook references: runbook:00986824035ce0aafb0d3c46c3d699ef", { exact: false })).toBeVisible();
  await expect(page.getByRole("button", { name: /^Evidence \[\d+\]/ })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
});

test("analytics labels insufficient samples and unavailable model output", async ({ page }) => {
  await installFixtureApi(page, "OPS_VIEWER");
  await page.route("http://localhost:8080/api/ops/analytics/windows**", async (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    return fulfillFixture(route, { items: [{
      id: "window-fixture", entityType: "bank", entityId: "BANK-A", from: "2026-10-11T00:00:00Z", to: "2026-10-11T00:05:00Z",
      sampleCount: 2, featureSchemaVersion: "fixture.v1", features: { timeoutRate: null, failureRate: null, p95LatencyMs: null, oldestRecoveryAgeMs: null, recoveryAttemptsPerPayment: null, healthAvailabilityRate: null },
      baselineAlert: { code: "INSUFFICIENT_SAMPLES", message: "Too few observations" },
      model: { version: null, status: "insufficient_data", score: null, threshold: null }, advisory: true,
    }], nextCursor: null, asOf: "2026-10-11T00:05:00Z" });
  });
  await page.goto("/console/analytics");
  await expect(page.getByRole("heading", { name: "Operational analytics" })).toBeVisible();
  await expect(page.getByText("Too few observations")).toBeVisible();
  await expect(page.getByText("insufficient data · no version")).toBeVisible();
  await expect(page.getByText("Unavailable / Unavailable")).toBeVisible();
});
