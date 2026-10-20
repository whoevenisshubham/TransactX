import { test } from "node:test";
import assert from "node:assert/strict";
import { analyticsSeries } from "./AnalyticsCharts";
import type { AnalyticsWindow } from "./contracts";

const window: AnalyticsWindow = { id: "one", entityType: "bank", entityId: "BANK-A", from: "2026-10-11T00:00:00Z", to: "2026-10-11T00:05:00Z", sampleCount: 2, featureSchemaVersion: "v1", features: { timeoutRate: null, failureRate: 0, p95LatencyMs: null, oldestRecoveryAgeMs: null, recoveryAttemptsPerPayment: null, healthAvailabilityRate: null }, baselineAlert: null, model: { version: null, status: "unavailable", score: null, threshold: null }, advisory: true };

test("analytics never combines banks, execution targets, durations or schemas", () => {
  const groups = analyticsSeries([window, { ...window, id: "two", entityId: "BANK-B" }, { ...window, id: "three", entityType: "execution_target" }, { ...window, id: "four", to: "2026-10-11T00:10:00Z" }, { ...window, id: "five", featureSchemaVersion: "v2" }]);
  assert.equal(groups.length, 5);
  assert.deepEqual(groups.map(group => group.items.length), [1, 1, 1, 1, 1]);
});

test("time ordering preserves genuine gaps, null measurements and input order", () => {
  const later = { ...window, id: "later", from: "2026-10-11T01:00:00Z", to: "2026-10-11T01:05:00Z" };
  const input = [later, window];
  const groups = analyticsSeries(input);
  assert.deepEqual(groups[0].items.map(item => item.id), ["one", "later"]);
  assert.deepEqual(input.map(item => item.id), ["later", "one"]);
  assert.equal(groups[0].items[1].from, later.from);
  assert.equal(groups[0].items[0].features.timeoutRate, null);
  assert.equal(groups[0].items[0].features.failureRate, 0);
});

test("invalid intervals cannot become a plotted zero window", () => {
  assert.equal(analyticsSeries([{ ...window, to: window.from }, { ...window, from: "invalid" }]).length, 0);
});
