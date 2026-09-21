/**
 * merchant.test.ts
 *
 * Auth boundary and role guard tests for the merchant product slice.
 * Runs without a DOM or real API using Node's built-in test runner.
 */
import { describe, it } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// Helpers — replicate the minimal role / routing logic for unit testing
// without importing DOM-dependent React modules.
// ---------------------------------------------------------------------------

type Role = "CUSTOMER" | "MERCHANT" | "OPS_ADMIN" | string;

/** Mirror of App's role routing decision */
function resolveShell(role: Role): "customer" | "merchant" | "denied" {
  if (role === "MERCHANT") return "merchant";
  if (role === "CUSTOMER") return "customer";
  return "denied";
}

/** Mirror of readMerchantView() from main.tsx */
function readMerchantView(pathname: string): string {
  if (pathname.startsWith("/merchant/receive")) return "m-receive";
  if (pathname.startsWith("/merchant/incoming")) return "m-incoming";
  if (pathname.startsWith("/merchant/settlement")) return "m-settlement";
  if (pathname.startsWith("/merchant/search")) return "m-search";
  return "m-home";
}

/** Mirror of merchantViewPath() from main.tsx */
function merchantViewPath(view: string): string {
  switch (view) {
    case "m-receive": return "/merchant/receive";
    case "m-incoming": return "/merchant/incoming";
    case "m-settlement": return "/merchant/settlement";
    case "m-search": return "/merchant/search";
    default: return "/merchant";
  }
}

/** Simulate the merchant incoming filter */
function filterIncoming(payments: Array<{ direction: string; state: string }>) {
  return payments.filter((p) => p.direction === "RECEIVED");
}

/** Simulate the merchant search filter */
function filterSearch(
  payments: Array<{ direction: string; senderName: string; senderPaymentIdentifier: string; note?: string; state: string }>,
  query: string,
  stateFilter: string
) {
  return payments.filter((p) => {
    const matchQuery = query.trim() === "" ||
      p.senderName.toLowerCase().includes(query.toLowerCase()) ||
      p.senderPaymentIdentifier.toLowerCase().includes(query.toLowerCase()) ||
      (p.note ?? "").toLowerCase().includes(query.toLowerCase());
    const matchState = stateFilter === "all" || p.state === stateFilter;
    return p.direction === "RECEIVED" && matchQuery && matchState;
  });
}

/** Simulate safe receive-info response fields */
const FORBIDDEN_FIELDS = [
  "senderAccountId", "receiverAccountId", "initiatedByUserId",
  "sourceBankId", "destinationBankId", "sourceBankAccountId",
  "destinationBankAccountId", "routeBankId", "bankAccountId",
];

function hasForbiddenFields(obj: Record<string, unknown>): string | null {
  for (const field of FORBIDDEN_FIELDS) {
    if (field in obj) return field;
  }
  return null;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("Role-based shell routing", () => {
  it("MERCHANT role routes to merchant shell", () => {
    assert.equal(resolveShell("MERCHANT"), "merchant");
  });

  it("CUSTOMER role routes to customer shell", () => {
    assert.equal(resolveShell("CUSTOMER"), "customer");
  });

  it("OPS_ADMIN routes to denied screen", () => {
    assert.equal(resolveShell("OPS_ADMIN"), "denied");
  });

  it("Unknown role routes to denied screen", () => {
    assert.equal(resolveShell("SUPERUSER"), "denied");
    assert.equal(resolveShell(""), "denied");
  });

  it("MERCHANT and CUSTOMER are mutually exclusive shells", () => {
    assert.notEqual(resolveShell("MERCHANT"), resolveShell("CUSTOMER"));
  });
});

describe("Merchant view routing", () => {
  it("root merchant path resolves to m-home", () => {
    assert.equal(readMerchantView("/merchant"), "m-home");
    assert.equal(readMerchantView("/"), "m-home");
  });

  it("each sub-path resolves to the correct view key", () => {
    assert.equal(readMerchantView("/merchant/receive"), "m-receive");
    assert.equal(readMerchantView("/merchant/incoming"), "m-incoming");
    assert.equal(readMerchantView("/merchant/settlement"), "m-settlement");
    assert.equal(readMerchantView("/merchant/search"), "m-search");
  });

  it("merchantViewPath produces stable paths for each view", () => {
    assert.equal(merchantViewPath("m-receive"), "/merchant/receive");
    assert.equal(merchantViewPath("m-incoming"), "/merchant/incoming");
    assert.equal(merchantViewPath("m-settlement"), "/merchant/settlement");
    assert.equal(merchantViewPath("m-search"), "/merchant/search");
    assert.equal(merchantViewPath("m-home"), "/merchant");
  });

  it("view path and read are inverse for all views", () => {
    const views = ["m-receive", "m-incoming", "m-settlement", "m-search"];
    for (const view of views) {
      const path = merchantViewPath(view);
      assert.equal(readMerchantView(path), view, `round-trip failed for view ${view}`);
    }
  });
});

describe("Merchant incoming payment filter", () => {
  const payments = [
    { direction: "RECEIVED", state: "COMPLETED" },
    { direction: "SENT", state: "COMPLETED" },
    { direction: "RECEIVED", state: "FAILED" },
    { direction: "SENT", state: "PROCESSING" },
  ];

  it("filters to RECEIVED direction only", () => {
    const result = filterIncoming(payments);
    assert.equal(result.length, 2);
    assert.ok(result.every((p) => p.direction === "RECEIVED"));
  });

  it("returns empty array when no payments received", () => {
    const sentOnly = payments.filter((p) => p.direction === "SENT");
    assert.equal(filterIncoming(sentOnly).length, 0);
  });
});

describe("Merchant search filter", () => {
  const payments = [
    { direction: "RECEIVED", senderName: "Alice Commerce", senderPaymentIdentifier: "alice@transactx", note: "Invoice #001", state: "COMPLETED" },
    { direction: "RECEIVED", senderName: "Bob Retail", senderPaymentIdentifier: "bob@transactx", note: undefined, state: "FAILED" },
    { direction: "RECEIVED", senderName: "Carol Shop", senderPaymentIdentifier: "carol@transactx", note: "Monthly sub", state: "COMPLETED" },
    { direction: "SENT", senderName: "Merchant Itself", senderPaymentIdentifier: "merchant@transactx", note: undefined, state: "COMPLETED" },
  ];

  it("returns all RECEIVED payments when query and state are empty", () => {
    const result = filterSearch(payments, "", "all");
    assert.equal(result.length, 3);
  });

  it("filters by sender name (case-insensitive)", () => {
    const result = filterSearch(payments, "alice", "all");
    assert.equal(result.length, 1);
    assert.equal(result[0].senderName, "Alice Commerce");
  });

  it("filters by payment identifier", () => {
    const result = filterSearch(payments, "bob@transactx", "all");
    assert.equal(result.length, 1);
    assert.equal(result[0].senderPaymentIdentifier, "bob@transactx");
  });

  it("filters by note content", () => {
    const result = filterSearch(payments, "Invoice", "all");
    assert.equal(result.length, 1);
    assert.equal(result[0].note, "Invoice #001");
  });

  it("state filter COMPLETED returns only COMPLETED payments", () => {
    const result = filterSearch(payments, "", "COMPLETED");
    assert.equal(result.length, 2);
    assert.ok(result.every((p) => p.state === "COMPLETED"));
  });

  it("state filter FAILED returns only FAILED payments", () => {
    const result = filterSearch(payments, "", "FAILED");
    assert.equal(result.length, 1);
    assert.equal(result[0].senderPaymentIdentifier, "bob@transactx");
  });

  it("SENT direction payments are always excluded regardless of query", () => {
    // Even searching for 'Merchant Itself' (which is SENT) returns nothing
    const result = filterSearch(payments, "Merchant", "all");
    assert.equal(result.length, 0);
  });

  it("combined query and state filter narrows results correctly", () => {
    const result = filterSearch(payments, "carol", "COMPLETED");
    assert.equal(result.length, 1);
    assert.equal(result[0].senderName, "Carol Shop");
  });

  it("no results for unmatched query", () => {
    const result = filterSearch(payments, "zzz-no-match", "all");
    assert.equal(result.length, 0);
  });
});

describe("Merchant receive-info response field safety", () => {
  it("merchantReceiveInfo response shape contains no forbidden internal fields", () => {
    // Simulate a correct response from GET /api/merchant/receive-info
    const response: Record<string, unknown> = {
      paymentIdentifier: "merchant@transactx",
      accountNumber: "TXACC-001",
      accountStatus: "ACTIVE",
    };
    const forbidden = hasForbiddenFields(response);
    assert.equal(forbidden, null, `Response must not contain internal field: ${forbidden}`);
  });

  it("response with internal fields is correctly detected as unsafe", () => {
    const leakyResponse: Record<string, unknown> = {
      paymentIdentifier: "merchant@transactx",
      accountNumber: "TXACC-001",
      accountStatus: "ACTIVE",
      // This would be a leak:
      initiatedByUserId: "some-uuid",
    };
    const forbidden = hasForbiddenFields(leakyResponse);
    assert.equal(forbidden, "initiatedByUserId");
  });

  it("all forbidden fields are detected individually", () => {
    for (const field of FORBIDDEN_FIELDS) {
      const leaky: Record<string, unknown> = { [field]: "leak" };
      assert.equal(hasForbiddenFields(leaky), field, `Should detect forbidden field: ${field}`);
    }
  });
});

describe("Settlement state classification", () => {
  const allStates = [
    { state: "COMPLETED", expected: "completed" },
    { state: "FAILED", expected: "failed" },
    { state: "REVERSED", expected: "failed" },
    { state: "PROCESSING", expected: "pending" },
    { state: "PENDING_RECONCILIATION", expected: "pending" },
    { state: "BANK_SETTLED_CENTRAL_PENDING", expected: "pending" },
    { state: "CREATED", expected: "other" },
    { state: "QUEUED", expected: "other" },
  ];

  function classifySettlement(state: string): "completed" | "pending" | "failed" | "other" {
    if (state === "COMPLETED") return "completed";
    if (["FAILED", "REVERSED"].includes(state)) return "failed";
    if (["PROCESSING", "PENDING_RECONCILIATION", "BANK_SETTLED_CENTRAL_PENDING"].includes(state)) return "pending";
    return "other";
  }

  for (const { state, expected } of allStates) {
    it(`classifies ${state} as ${expected}`, () => {
      assert.equal(classifySettlement(state), expected);
    });
  }
});
