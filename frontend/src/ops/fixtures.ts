import { decodeEvidence, decodeWorklist } from "./contracts";
import type { OpsReadClient } from "./client";

/** Explicit test/development adapter. The application never selects it on failure. */
export function createFixtureOpsClient(fixtures: { worklist: unknown; evidence: unknown }): OpsReadClient {
  const worklist = decodeWorklist(fixtures.worklist);
  const evidence = decodeEvidence(fixtures.evidence);
  return {
    async worklist() { return worklist; },
    async evidence(paymentId) {
      if (paymentId !== evidence.paymentId) throw new Error("No fixture for this payment.");
      return evidence;
    },
  };
}
