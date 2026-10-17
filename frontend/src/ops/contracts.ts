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
  snapshot?: EvidenceBundle;
  toolTimeline: Array<{ toolName: string; arguments: Record<string, unknown>; startedAt: string; finishedAt: string | null; status: string; evidenceIds: string[] }>;
  error: { code: string; message: string } | null; createdAt: string;
  completedAt: string | null; usage: { inputTokens: number | null; outputTokens: number | null; model: string | null; promptVersion: string };
};
export type EvidenceAssertion = { field: string; value: string; entityId: string | null; evidenceId: string };
export type InvestigationReport = {
  schemaVersion: string; mode: "evidence_only" | "llm"; summary: string;
  facts: Array<{ text: string; evidenceIds: string[]; assertions: EvidenceAssertion[]; contradiction: boolean }>;
  inferences: Array<{ text: string; evidenceIds: string[]; contradiction: boolean }>;
  recommendations: Array<{ text: string; runbookChunkIds: string[] }>;
  missingEvidence: string[]; evidenceCoverage: "complete_for_question" | "limited" | "insufficient";
  snapshotId: string; collectedAt: string;
};
export type AnalyticsWindow = {
  id: string; entityType: "bank" | "execution_target"; entityId: string;
  from: string; to: string; sampleCount: number; featureSchemaVersion: string;
  features: { timeoutRate: number | null; failureRate: number | null; p95LatencyMs: number | null; oldestRecoveryAgeMs: number | null; recoveryAttemptsPerPayment: number | null; healthAvailabilityRate: number | null };
  baselineAlert: { code: string; message: string } | null;
  model: { version: string | null; status: "ready" | "insufficient_data" | "stale" | "unavailable" | "out_of_distribution"; score: number | null; threshold: number | null };
  advisory: true;
};
export type Trace = { traceId: string; sourceStatus: SourceStatus; collectedAt: string; truncated: boolean; spans: Array<{ spanId: string; parentSpanId: string | null; service: string; name: string; startedAt: string; finishedAt: string | null; status: string; attributes: Record<string, unknown> }> };

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
function finiteNumber(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isFinite(value)) throw new ContractError(path);
  return value;
}
function nullableNumber(value: unknown, path: string): number | null {
  return value === null ? null : finiteNumber(value, path);
}
function bool(value: unknown, path: string): boolean {
  if (typeof value !== "boolean") throw new ContractError(path);
  return value;
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
    transitionsTruncated: bool(root.transitionsTruncated, "transitionsTruncated"),
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
      scopeMismatch: bool(reconciliation.scopeMismatch, "reconciliation.scopeMismatch"),
    },
    integrity: {
      sourceStatus: oneOf(integrity.sourceStatus, sourceStatuses, "integrity.sourceStatus"),
      runId: nullableString(integrity.runId, "integrity.runId"), occurredAt: nullableString(integrity.occurredAt, "integrity.occurredAt"),
      violationCount: integrity.violationCount === null ? null : integer(integrity.violationCount, "integrity.violationCount"),
    },
    traces: {
      sourceStatus: oneOf(traces.sourceStatus, sourceStatuses, "traces.sourceStatus"),
      traceIds: array(traces.traceIds, "traces.traceIds", string),
      truncated: bool(traces.truncated, "traces.truncated"),
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

export function decodeInvestigation(value: unknown): Investigation {
  const root = object(value, "investigation");
  const usage = object(root.usage, "usage");
  let report: InvestigationReport | null = null;
  if (root.report !== null) {
    const raw = object(root.report, "report");
    report = {
      schemaVersion: string(raw.schemaVersion, "report.schemaVersion"),
      mode: oneOf(raw.mode, ["evidence_only", "llm"], "report.mode"),
      summary: string(raw.summary, "report.summary"),
      facts: array(raw.facts, "report.facts", (value, path) => {
        const item = object(value, path);
        return {
          text: string(item.text, `${path}.text`), evidenceIds: array(item.evidenceIds, `${path}.evidenceIds`, string),
          assertions: array(item.assertions, `${path}.assertions`, (value, path) => {
            const assertion = object(value, path);
            return { field: string(assertion.field, `${path}.field`), value: string(assertion.value, `${path}.value`), entityId: nullableString(assertion.entityId, `${path}.entityId`), evidenceId: string(assertion.evidenceId, `${path}.evidenceId`) };
          }),
          contradiction: bool(item.contradiction, `${path}.contradiction`),
        };
      }),
      inferences: array(raw.inferences, "report.inferences", (value, path) => {
        const item = object(value, path);
        return { text: string(item.text, `${path}.text`), evidenceIds: array(item.evidenceIds, `${path}.evidenceIds`, string), contradiction: bool(item.contradiction, `${path}.contradiction`) };
      }),
      recommendations: array(raw.recommendations, "report.recommendations", (value, path) => {
        const item = object(value, path);
        return { text: string(item.text, `${path}.text`), runbookChunkIds: array(item.runbookChunkIds, `${path}.runbookChunkIds`, string) };
      }),
      missingEvidence: array(raw.missingEvidence, "report.missingEvidence", string),
      evidenceCoverage: oneOf(raw.evidenceCoverage, ["complete_for_question", "limited", "insufficient"], "report.evidenceCoverage"),
      snapshotId: string(raw.snapshotId, "report.snapshotId"), collectedAt: string(raw.collectedAt, "report.collectedAt"),
    };
  }
  const error = root.error === null ? null : (() => {
    const warning = object(root.error, "error");
    return { code: string(warning.code, "error.code"), message: string(warning.message, "error.message") };
  })();
  const snapshot = root.snapshot !== undefined && root.snapshot !== null ? decodeEvidence(root.snapshot) : undefined;
  return {
    id: string(root.id, "id"), status: oneOf(root.status, ["queued", "running", "completed", "failed", "cancelled"], "status"),
    mode: oneOf(root.mode, ["evidence_only", "llm"], "mode"), report, snapshot,
    toolTimeline: array(root.toolTimeline, "toolTimeline", (value, path) => {
      const step = object(value, path);
      return { toolName: string(step.toolName, `${path}.toolName`), arguments: object(step.arguments, `${path}.arguments`), startedAt: string(step.startedAt, `${path}.startedAt`), finishedAt: nullableString(step.finishedAt, `${path}.finishedAt`), status: string(step.status, `${path}.status`), evidenceIds: array(step.evidenceIds, `${path}.evidenceIds`, string) };
    }),
    error, createdAt: string(root.createdAt, "createdAt"), completedAt: nullableString(root.completedAt, "completedAt"),
    usage: { inputTokens: usage.inputTokens === null ? null : integer(usage.inputTokens, "usage.inputTokens"), outputTokens: usage.outputTokens === null ? null : integer(usage.outputTokens, "usage.outputTokens"), model: nullableString(usage.model, "usage.model"), promptVersion: string(usage.promptVersion, "usage.promptVersion") },
  };
}

export function decodeAnalyticsPage(value: unknown): CursorPage<AnalyticsWindow> {
  const root = object(value, "analytics");
  return {
    items: array(root.items, "analytics.items", (value, path) => {
      const window = object(value, path);
      const features = object(window.features, `${path}.features`);
      const model = object(window.model, `${path}.model`);
      const rawAlert = window.baselineAlert;
      const baselineAlert = rawAlert === null ? null : object(rawAlert, `${path}.baselineAlert`);
      return {
        id: string(window.id, `${path}.id`), entityType: oneOf(window.entityType, ["bank", "execution_target"], `${path}.entityType`),
        entityId: string(window.entityId, `${path}.entityId`), from: string(window.from, `${path}.from`), to: string(window.to, `${path}.to`),
        sampleCount: integer(window.sampleCount, `${path}.sampleCount`), featureSchemaVersion: string(window.featureSchemaVersion, `${path}.featureSchemaVersion`),
        features: {
          timeoutRate: nullableNumber(features.timeoutRate, `${path}.features.timeoutRate`), failureRate: nullableNumber(features.failureRate, `${path}.features.failureRate`),
          p95LatencyMs: nullableNumber(features.p95LatencyMs, `${path}.features.p95LatencyMs`), oldestRecoveryAgeMs: nullableNumber(features.oldestRecoveryAgeMs, `${path}.features.oldestRecoveryAgeMs`),
          recoveryAttemptsPerPayment: nullableNumber(features.recoveryAttemptsPerPayment, `${path}.features.recoveryAttemptsPerPayment`), healthAvailabilityRate: nullableNumber(features.healthAvailabilityRate, `${path}.features.healthAvailabilityRate`),
        },
        baselineAlert: baselineAlert === null ? null : { code: string(baselineAlert.code, `${path}.baselineAlert.code`), message: string(baselineAlert.message, `${path}.baselineAlert.message`) },
        model: { version: nullableString(model.version, `${path}.model.version`), status: oneOf(model.status, ["ready", "insufficient_data", "stale", "unavailable", "out_of_distribution"], `${path}.model.status`), score: nullableNumber(model.score, `${path}.model.score`), threshold: nullableNumber(model.threshold, `${path}.model.threshold`) },
        advisory: window.advisory === true ? true as const : (() => { throw new ContractError(`${path}.advisory`); })(),
      };
    }),
    nextCursor: nullableString(root.nextCursor, "analytics.nextCursor"), asOf: string(root.asOf, "analytics.asOf"),
  };
}

export function decodeTrace(value: unknown): Trace {
  const root = object(value, "trace");
  return {
    traceId: string(root.traceId, "traceId"), sourceStatus: oneOf(root.sourceStatus, sourceStatuses, "sourceStatus"),
    collectedAt: string(root.collectedAt, "collectedAt"), truncated: bool(root.truncated, "truncated"),
    spans: array(root.spans, "spans", (value, path) => {
      const span = object(value, path);
      return { spanId: string(span.spanId, `${path}.spanId`), parentSpanId: nullableString(span.parentSpanId, `${path}.parentSpanId`), service: string(span.service, `${path}.service`), name: string(span.name, `${path}.name`), startedAt: string(span.startedAt, `${path}.startedAt`), finishedAt: nullableString(span.finishedAt, `${path}.finishedAt`), status: string(span.status, `${path}.status`), attributes: object(span.attributes, `${path}.attributes`) };
    }),
  };
}
