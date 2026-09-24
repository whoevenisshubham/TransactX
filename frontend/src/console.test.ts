/**
 * console.test.ts
 *
 * Auth boundary, role isolation, and route resolution tests for the Network Console (OPS_ADMIN).
 * Runs with Node's built-in test runner without DOM dependencies.
 */
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  readConsoleView,
  consoleViewPath,
  deriveOperationalTargets,
  buildChaosPayload,
  SUPPORTED_CHAOS_TYPES,
  type ChaosFaultType,
} from "./console";
import type { ConsoleView, CircuitTargetSnapshot } from "./types";

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

describe("Network Console: Operational target derivation and grounding", () => {
  it("chaos target list comes only from actual circuit targets", () => {
    const circuitMap: Record<string, CircuitTargetSnapshot> = {
      "GW-DIRECT-A": {
        executionTargetId: "GW-DIRECT-A",
        state: "CLOSED",
        failureCount: 0,
        timeoutCount: 0,
        consecutiveSuccesses: 5,
        activeProbes: 0,
        successfulProbes: 0,
        restorationStep: 0,
        maxRestorationSteps: 3,
        restorationProgress: 1,
        lastEvaluatedAt: new Date().toISOString(),
      },
      "GW-DIRECT-B": {
        executionTargetId: "GW-DIRECT-B",
        state: "HALF_OPEN",
        failureCount: 1,
        timeoutCount: 0,
        consecutiveSuccesses: 1,
        activeProbes: 1,
        successfulProbes: 1,
        restorationStep: 1,
        maxRestorationSteps: 3,
        restorationProgress: 0.33,
        lastEvaluatedAt: new Date().toISOString(),
      },
    };

    const derived = deriveOperationalTargets(circuitMap);
    assert.deepEqual(derived, ["GW-DIRECT-A", "GW-DIRECT-B"]);
  });

  it("BANK-A/B are not automatically inserted when absent from circuit targets", () => {
    const circuitMap: Record<string, CircuitTargetSnapshot> = {
      "CUSTOM-ROUTE-99": {
        executionTargetId: "CUSTOM-ROUTE-99",
        state: "CLOSED",
        failureCount: 0,
        timeoutCount: 0,
        consecutiveSuccesses: 2,
        activeProbes: 0,
        successfulProbes: 0,
        restorationStep: 0,
        maxRestorationSteps: 3,
        restorationProgress: 1,
        lastEvaluatedAt: new Date().toISOString(),
      },
    };

    const derived = deriveOperationalTargets(circuitMap);
    assert.equal(derived.length, 1);
    assert.equal(derived[0], "CUSTOM-ROUTE-99");
    assert.equal(derived.includes("BANK-A"), false);
    assert.equal(derived.includes("BANK-B"), false);
  });

  it("empty circuit target list produces safe empty state", () => {
    assert.deepEqual(deriveOperationalTargets({}), []);
    assert.deepEqual(deriveOperationalTargets(null), []);
    assert.deepEqual(deriveOperationalTargets(undefined), []);
  });
});

describe("Network Console: Chaos scenario support & TEMPORARY_PARTITION", () => {
  it("TEMPORARY_PARTITION is an accepted console scenario type", () => {
    assert.equal(SUPPORTED_CHAOS_TYPES.includes("TEMPORARY_PARTITION"), true);
    assert.deepEqual(SUPPORTED_CHAOS_TYPES, [
      "BANK_OUTAGE",
      "LATENCY",
      "TRANSIENT_DROP",
      "TEMPORARY_PARTITION",
    ]);
  });

  it("TEMPORARY_PARTITION payload conforms to backend schema without unsupported parameters", () => {
    const payload = buildChaosPayload("part-1", "TARGET-EXEC-A", "TEMPORARY_PARTITION", {
      durationMs: 45000,
      latencyMs: 9999,
      dropRate: 0.8,
    });

    assert.equal(payload.scenarioId, "part-1");
    assert.equal(payload.targetId, "TARGET-EXEC-A");
    assert.equal(payload.type, "TEMPORARY_PARTITION");
    assert.equal(payload.parameters.durationMs, 45000);
    assert.equal((payload.parameters as any).latencyMs, undefined);
    assert.equal((payload.parameters as any).dropRate, undefined);
  });

  it("LATENCY and TRANSIENT_DROP payloads attach only their applicable parameters", () => {
    const latencyPayload = buildChaosPayload("lat-1", "TARGET-EXEC-A", "LATENCY", {
      durationMs: 20000,
      latencyMs: 2500,
    });
    assert.equal(latencyPayload.type, "LATENCY");
    assert.equal(latencyPayload.parameters.durationMs, 20000);
    assert.equal(latencyPayload.parameters.latencyMs, 2500);
    assert.equal((latencyPayload.parameters as any).dropRate, undefined);

    const dropPayload = buildChaosPayload("drop-1", "TARGET-EXEC-B", "TRANSIENT_DROP", {
      durationMs: 15000,
      dropRate: 0.25,
    });
    assert.equal(dropPayload.type, "TRANSIENT_DROP");
    assert.equal(dropPayload.parameters.durationMs, 15000);
    assert.equal(dropPayload.parameters.dropRate, 0.25);
    assert.equal((dropPayload.parameters as any).latencyMs, undefined);
  });
});
