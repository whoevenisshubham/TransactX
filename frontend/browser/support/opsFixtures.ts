import { readFileSync } from "node:fs";
import type { Page, Route } from "@playwright/test";

export const paymentId = "11111111-1111-4111-8111-111111111111";
export const investigationId = "44444444-4444-4444-8444-444444444444";
export function fixture(name: string) {
  return JSON.parse(readFileSync(new URL(`../../../docs/next-gen/execution-pack/docs/next-gen/contracts/fixtures/${name}`, import.meta.url), "utf8"));
}

export function worklistFixture() {
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

export function fulfillFixture(route: Route, data: unknown, status = 200) {
  return route.fulfill({ status, headers: { "access-control-allow-origin": "http://127.0.0.1:4173" }, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", data }) });
}

export async function installFixtureApi(page: Page, role: "OPS_VIEWER" | "OPS_ADMIN", onRecheck?: (key: string, attempt: number) => boolean, onInvestigation?: (key: string, attempt: number) => boolean) {
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

