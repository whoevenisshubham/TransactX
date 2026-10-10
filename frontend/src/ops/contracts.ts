/** V2 operational DTOs. Money and version counters remain decimal strings. */
export type SourceStatus = "available" | "unavailable" | "not_applicable" | "not_collected";
export type RecoveryStatus = "none" | "scheduled" | "running" | "deferred" | "needs_attention" | "resolved";
export type ProjectionStatus = "ready" | "lagging" | "rebuilding" | "unavailable";

export type CursorPage<T> = { items: T[]; nextCursor: string | null; asOf: string };
export type WorklistItem = {
  paymentId: string; state: string; amountPaise: string; currency: string;
  sourceBankCode: string | null; destinationBankCode: string | null;
  createdAt: string; updatedAt: string; ageMs: number;
  recoveryStatus: RecoveryStatus; attemptCount: number; nextAttemptAt: string | null;
  reasonCode: string | null; projectionVersion: string;
};
export type Worklist = CursorPage<WorklistItem> & {
  projection: { status: ProjectionStatus; updatedAt: string; oldestPendingEventAgeMs: number | null };
};
export type WorklistFilters = {
  state?: string; recoveryStatus?: RecoveryStatus; bankCode?: string;
  paymentId?: string; createdFrom?: string; createdTo?: string;
  cursor?: string; limit?: number;
};
export type EvidenceSource = { name: string; status: SourceStatus; observedAt: string | null; errorCode: string | null };
export type EvidenceOperation = {
  operationId: string; operationType: string; bankCode: string; centralStatus: string;
  participantStatus: string | null; presence: "present" | "absent_confirmed" | "unknown" | "not_checked";
  lastObservedAt: string | null; sourceStatus: SourceStatus; evidenceId: string;
};
export type EvidenceTransition = {
  id: string; sequence: string; fromState: string; toState: string;
  occurredAt: string; evidenceId: string;
};
export type EvidenceBundle = {
  schemaVersion: string; paymentId: string; snapshotId: string; collectedAt: string;
  payment: { state: string; amountPaise: string; currency: string; createdAt: string; updatedAt: string; completedAt: string | null; eventVersion: string; evidenceId: string };
  route: { sourceBankCode: string | null; destinationBankCode: string | null; executionTargetId: string | null; selectedAt: string | null; reasonCode: string | null; status: SourceStatus };
  operations: EvidenceOperation[]; transitions: EvidenceTransition[]; transitionsTruncated: boolean;
  recovery: { status: RecoveryStatus; attemptCount: number; nextAttemptAt: string | null; lastAttemptId: string | null; reasonCode: string | null; sourceStatus: SourceStatus };
  reconciliation: { sourceStatus: SourceStatus; runIds: string[]; discrepancyCount: number | null; scopeMismatch: boolean };
  integrity: { sourceStatus: SourceStatus; runId: string | null; occurredAt: string | null; violationCount: number | null };
  traces: { sourceStatus: SourceStatus; traceIds: string[]; truncated: boolean };
  sources: EvidenceSource[]; warnings: { code: string; message: string }[];
};
export type RecoveryAttempt = {
  attemptId: string; paymentId: string; workerId: string; startedAt: string;
  finishedAt: string | null; stateBefore: string; stateAfter: string | null;
  result: "running" | "progressed" | "resolved" | "no_progress" | "deferred" | "error" | "interrupted";
  reasonCode: string | null; operationIds: string[]; traceId: string | null; leaseEpoch: string;
};
export type Investigation = {
  id: string; status: "queued" | "running" | "completed" | "failed" | "cancelled";
  mode: "evidence_only" | "llm"; report: InvestigationReport | null;
  toolTimeline: Array<{ toolName: string; startedAt: string; finishedAt: string | null; resultStatus: string; evidenceIds: string[] }>;
  error: { code: string; message: string } | null; createdAt: string;
  completedAt: string | null; usage: unknown;
};
export type InvestigationReport = {
  schemaVersion: string; mode: "evidence_only" | "llm"; summary: string;
  facts: Array<{ text: string; evidenceIds: string[]; contradiction: boolean }>;
  inferences: Array<{ text: string; evidenceIds: string[]; contradiction: boolean }>;
  recommendations: Array<{ text: string; runbookChunkIds: string[] }>;
  missingEvidence: string[]; evidenceCoverage: "complete_for_question" | "limited" | "insufficient";
  snapshotId: string; collectedAt: string;
};
export type AnalyticsWindow = {
  id: string; entityType: "bank" | "execution_target"; entityId: string;
  from: string; to: string; sampleCount: number; featureSchemaVersion: string;
  features: Record<string, number | null>; baselineAlert: unknown;
  model: { version: string | null; status: "ready" | "insufficient_data" | "stale" | "unavailable" | "out_of_distribution"; score: number | null; threshold: number | null };
  advisory: true;
};

export class ContractError extends Error {
  constructor(path: string) { super(`The server returned an incompatible operational response at ${path}.`); this.name = "ContractError"; }
}
function object(value: unknown, path: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new ContractError(path);
  return value as Record<string, unknown>;
}
function string(value: unknown, path: string): string {
  if (typeof value !== "string") throw new ContractError(path);
  return value;
}
function nullableString(value: unknown, path: string): string | null {
  return value === null ? null : string(value, path);
}
function integer(value: unknown, path: string): number {
  if (!Number.isSafeInteger(value) || (value as number) < 0) throw new ContractError(path);
  return value as number;
}
function decimal(value: unknown, path: string): string {
  const result = string(value, path);
  if (!/^(0|[1-9][0-9]*)$/.test(result)) throw new ContractError(path);
  return result;
}
function oneOf<T extends string>(value: unknown, allowed: readonly T[], path: string): T {
  if (typeof value !== "string" || !allowed.includes(value as T)) throw new ContractError(path);
  return value as T;
}
function array<T>(value: unknown, path: string, decode: (value: unknown, path: string) => T): T[] {
  if (!Array.isArray(value)) throw new ContractError(path);
  return value.map((item, index) => decode(item, `${path}[${index}]`));
}
const sourceStatuses = ["available", "unavailable", "not_applicable", "not_collected"] as const;
const recoveryStatuses = ["none", "scheduled", "running", "deferred", "needs_attention", "resolved"] as const;

export function decodeWorklist(value: unknown): Worklist {
  const root = object(value, "worklist");
  const projection = object(root.projection, "projection");
  return {
    items: array(root.items, "items", (value, path) => {
      const item = object(value, path);
      return {
        paymentId: string(item.paymentId, `${path}.paymentId`), state: string(item.state, `${path}.state`),
        amountPaise: decimal(item.amountPaise, `${path}.amountPaise`), currency: string(item.currency, `${path}.currency`),
        sourceBankCode: nullableString(item.sourceBankCode, `${path}.sourceBankCode`),
        destinationBankCode: nullableString(item.destinationBankCode, `${path}.destinationBankCode`),
        createdAt: string(item.createdAt, `${path}.createdAt`), updatedAt: string(item.updatedAt, `${path}.updatedAt`),
        ageMs: integer(item.ageMs, `${path}.ageMs`),
        recoveryStatus: oneOf(item.recoveryStatus, recoveryStatuses, `${path}.recoveryStatus`),
        attemptCount: integer(item.attemptCount, `${path}.attemptCount`),
        nextAttemptAt: nullableString(item.nextAttemptAt, `${path}.nextAttemptAt`),
        reasonCode: nullableString(item.reasonCode, `${path}.reasonCode`),
        projectionVersion: decimal(item.projectionVersion, `${path}.projectionVersion`),
      };
    }),
    nextCursor: nullableString(root.nextCursor, "nextCursor"), asOf: string(root.asOf, "asOf"),
    projection: {
      status: oneOf(projection.status, ["ready", "lagging", "rebuilding", "unavailable"], "projection.status"),
      updatedAt: string(projection.updatedAt, "projection.updatedAt"),
      oldestPendingEventAgeMs: projection.oldestPendingEventAgeMs === null ? null : integer(projection.oldestPendingEventAgeMs, "projection.oldestPendingEventAgeMs"),
    },
  };
}

export function decodeEvidence(value: unknown): EvidenceBundle {
  const root = object(value, "evidence");
  const payment = object(root.payment, "payment");
  const route = object(root.route, "route");
  const recovery = object(root.recovery, "recovery");
  const reconciliation = object(root.reconciliation, "reconciliation");
  const integrity = object(root.integrity, "integrity");
  const traces = object(root.traces, "traces");
  return {
    schemaVersion: string(root.schemaVersion, "schemaVersion"), paymentId: string(root.paymentId, "paymentId"),
    snapshotId: string(root.snapshotId, "snapshotId"), collectedAt: string(root.collectedAt, "collectedAt"),
    payment: {
      state: string(payment.state, "payment.state"), amountPaise: decimal(payment.amountPaise, "payment.amountPaise"),
      currency: string(payment.currency, "payment.currency"), createdAt: string(payment.createdAt, "payment.createdAt"),
      updatedAt: string(payment.updatedAt, "payment.updatedAt"), completedAt: nullableString(payment.completedAt, "payment.completedAt"),
      eventVersion: decimal(payment.eventVersion, "payment.eventVersion"), evidenceId: string(payment.evidenceId, "payment.evidenceId"),
    },
    route: {
      sourceBankCode: nullableString(route.sourceBankCode, "route.sourceBankCode"),
      destinationBankCode: nullableString(route.destinationBankCode, "route.destinationBankCode"),
      executionTargetId: nullableString(route.executionTargetId, "route.executionTargetId"),
      selectedAt: nullableString(route.selectedAt, "route.selectedAt"), reasonCode: nullableString(route.reasonCode, "route.reasonCode"),
      status: oneOf(route.status, sourceStatuses, "route.status"),
    },
    operations: array(root.operations, "operations", (value, path) => {
      const item = object(value, path);
      return {
        operationId: string(item.operationId, `${path}.operationId`), operationType: string(item.operationType, `${path}.operationType`),
        bankCode: string(item.bankCode, `${path}.bankCode`), centralStatus: string(item.centralStatus, `${path}.centralStatus`),
        participantStatus: nullableString(item.participantStatus, `${path}.participantStatus`),
        presence: oneOf(item.presence, ["present", "absent_confirmed", "unknown", "not_checked"], `${path}.presence`),
        lastObservedAt: nullableString(item.lastObservedAt, `${path}.lastObservedAt`),
        sourceStatus: oneOf(item.sourceStatus, sourceStatuses, `${path}.sourceStatus`),
        evidenceId: string(item.evidenceId, `${path}.evidenceId`),
      };
    }),
    transitions: array(root.transitions, "transitions", (value, path) => {
      const item = object(value, path);
      return {
        id: string(item.id, `${path}.id`), sequence: decimal(item.sequence, `${path}.sequence`),
        fromState: string(item.fromState, `${path}.fromState`), toState: string(item.toState, `${path}.toState`),
        occurredAt: string(item.occurredAt, `${path}.occurredAt`), evidenceId: string(item.evidenceId, `${path}.evidenceId`),
      };
    }),
    transitionsTruncated: root.transitionsTruncated === true ? true : root.transitionsTruncated === false ? false : (() => { throw new ContractError("transitionsTruncated"); })(),
    recovery: {
      status: oneOf(recovery.status, recoveryStatuses, "recovery.status"), attemptCount: integer(recovery.attemptCount, "recovery.attemptCount"),
      nextAttemptAt: nullableString(recovery.nextAttemptAt, "recovery.nextAttemptAt"),
      lastAttemptId: nullableString(recovery.lastAttemptId, "recovery.lastAttemptId"),
      reasonCode: nullableString(recovery.reasonCode, "recovery.reasonCode"),
      sourceStatus: oneOf(recovery.sourceStatus, sourceStatuses, "recovery.sourceStatus"),
    },
    reconciliation: {
      sourceStatus: oneOf(reconciliation.sourceStatus, sourceStatuses, "reconciliation.sourceStatus"),
      runIds: array(reconciliation.runIds, "reconciliation.runIds", string),
      discrepancyCount: reconciliation.discrepancyCount === null ? null : integer(reconciliation.discrepancyCount, "reconciliation.discrepancyCount"),
      scopeMismatch: reconciliation.scopeMismatch === true ? true : reconciliation.scopeMismatch === false ? false : (() => { throw new ContractError("reconciliation.scopeMismatch"); })(),
    },
    integrity: {
      sourceStatus: oneOf(integrity.sourceStatus, sourceStatuses, "integrity.sourceStatus"),
      runId: nullableString(integrity.runId, "integrity.runId"), occurredAt: nullableString(integrity.occurredAt, "integrity.occurredAt"),
      violationCount: integrity.violationCount === null ? null : integer(integrity.violationCount, "integrity.violationCount"),
    },
    traces: {
      sourceStatus: oneOf(traces.sourceStatus, sourceStatuses, "traces.sourceStatus"),
      traceIds: array(traces.traceIds, "traces.traceIds", string),
      truncated: traces.truncated === true ? true : traces.truncated === false ? false : (() => { throw new ContractError("traces.truncated"); })(),
    },
    sources: array(root.sources, "sources", (value, path) => {
      const item = object(value, path);
      return { name: string(item.name, `${path}.name`), status: oneOf(item.status, sourceStatuses, `${path}.status`), observedAt: nullableString(item.observedAt, `${path}.observedAt`), errorCode: nullableString(item.errorCode, `${path}.errorCode`) };
    }),
    warnings: array(root.warnings, "warnings", (value, path) => {
      const item = object(value, path);
      return { code: string(item.code, `${path}.code`), message: string(item.message, `${path}.message`) };
    }),
  };
}
