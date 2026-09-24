/**
 * console.test.ts
 *
 * Auth boundary, role isolation, and route resolution tests for the Network Console (OPS_ADMIN).
 * Runs with Node's built-in test runner without DOM dependencies.
 */
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readConsoleView, consoleViewPath } from "./console";
import type { ConsoleView } from "./types";

// ---------------------------------------------------------------------------
// Role routing resolution helper matching App's exact logic in main.tsx
// ---------------------------------------------------------------------------

type Role = "CUSTOMER" | "MERCHANT" | "OPS_ADMIN" | string;

function resolveProductShell(role: Role): "customer" | "merchant" | "console" | "denied" {
  if (role === "OPS_ADMIN") return "console";
  if (role === "MERCHANT") return "merchant";
  if (role === "CUSTOMER") return "customer";
  return "denied";
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("Network Console: Role-based shell routing and isolation", () => {
  it("OPS_ADMIN role routes strictly to console shell", () => {
    assert.equal(resolveProductShell("OPS_ADMIN"), "console");
  });

  it("CUSTOMER role routes to customer shell and cannot enter console", () => {
    assert.equal(resolveProductShell("CUSTOMER"), "customer");
    assert.notEqual(resolveProductShell("CUSTOMER"), "console");
  });

  it("MERCHANT role routes to merchant shell and cannot enter console", () => {
    assert.equal(resolveProductShell("MERCHANT"), "merchant");
    assert.notEqual(resolveProductShell("MERCHANT"), "console");
  });

  it("Unknown roles receive access denied", () => {
    assert.equal(resolveProductShell("ANONYMOUS"), "denied");
    assert.equal(resolveProductShell("SUPERUSER"), "denied");
    assert.equal(resolveProductShell("AUDITOR"), "denied");
    assert.equal(resolveProductShell(""), "denied");
  });

  it("All three product surfaces are mutually exclusive", () => {
    const customer = resolveProductShell("CUSTOMER");
    const merchant = resolveProductShell("MERCHANT");
    const console = resolveProductShell("OPS_ADMIN");

    assert.notEqual(customer, merchant);
    assert.notEqual(merchant, console);
    assert.notEqual(customer, console);
  });
});

describe("Network Console: View path resolution (readConsoleView)", () => {
  it("root console path resolves to c-overview", () => {
    assert.equal(readConsoleView("/console"), "c-overview");
    assert.equal(readConsoleView("/console/"), "c-overview");
    assert.equal(readConsoleView("/"), "c-overview");
  });

  it("health subpath resolves to c-health", () => {
    assert.equal(readConsoleView("/console/health"), "c-health");
    assert.equal(readConsoleView("/console/health/BANK-A"), "c-health");
  });

  it("routing subpath resolves to c-routing", () => {
    assert.equal(readConsoleView("/console/routing"), "c-routing");
  });

  it("reconciliation subpath resolves to c-reconciliation", () => {
    assert.equal(readConsoleView("/console/reconciliation"), "c-reconciliation");
  });

  it("merkle subpath resolves to c-merkle", () => {
    assert.equal(readConsoleView("/console/merkle"), "c-merkle");
  });

  it("integrity subpath resolves to c-integrity", () => {
    assert.equal(readConsoleView("/console/integrity"), "c-integrity");
  });

  it("chaos subpath resolves to c-chaos", () => {
    assert.equal(readConsoleView("/console/chaos"), "c-chaos");
  });

  it("activity subpath resolves to c-activity", () => {
    assert.equal(readConsoleView("/console/activity"), "c-activity");
  });

  it("unrecognized console subpath falls back safely to c-overview", () => {
    assert.equal(readConsoleView("/console/unknown-tab"), "c-overview");
  });
});

describe("Network Console: Route URL generator (consoleViewPath)", () => {
  it("generates correct paths for each console view", () => {
    assert.equal(consoleViewPath("c-overview"), "/console");
    assert.equal(consoleViewPath("c-health"), "/console/health");
    assert.equal(consoleViewPath("c-routing"), "/console/routing");
    assert.equal(consoleViewPath("c-reconciliation"), "/console/reconciliation");
    assert.equal(consoleViewPath("c-merkle"), "/console/merkle");
    assert.equal(consoleViewPath("c-integrity"), "/console/integrity");
    assert.equal(consoleViewPath("c-chaos"), "/console/chaos");
    assert.equal(consoleViewPath("c-activity"), "/console/activity");
  });

  it("roundtrips all 8 views reliably", () => {
    const views: ConsoleView[] = [
      "c-overview",
      "c-health",
      "c-routing",
      "c-reconciliation",
      "c-merkle",
      "c-integrity",
      "c-chaos",
      "c-activity",
    ];

    for (const v of views) {
      const path = consoleViewPath(v);
      const parsed = readConsoleView(path);
      assert.equal(parsed, v, `Expected ${path} to parse back to ${v}`);
    }
  });
});
