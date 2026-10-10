import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseWorklistFilters, worklistSearch } from "./worklistFilters";

describe("URL-backed worklist filters", () => {
  it("round-trips a state, UUID and cursor for refresh and history navigation", () => {
    const search = worklistSearch({ state: "PENDING_RECONCILIATION", paymentId: "11111111-1111-4111-8111-111111111111", cursor: "abc+/=", limit: 25 });
    const result = parseWorklistFilters(search);
    assert.equal(result.warning, null);
    assert.equal(result.filters.cursor, "abc+/=");
    assert.equal(result.filters.state, "PENDING_RECONCILIATION");
  });
  it("rejects invalid filters before they reach the API", () => {
    const result = parseWorklistFilters("?paymentId=not-a-uuid&state=UNKNOWN&limit=1000");
    assert.equal(result.filters.paymentId, undefined);
    assert.equal(result.filters.state, undefined);
    assert.equal(result.filters.limit, undefined);
    assert.ok(result.warning);
  });
});
