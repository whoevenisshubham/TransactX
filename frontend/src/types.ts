export type User = {
  id: string;
  name: string;
  paymentIdentifier: string;
  role: string;
};

export type Account = {
  accountNumber: string;
  balancePaise: number;
  status: string;
};

export type Recipient = {
  name: string;
  paymentIdentifier: string;
  status: string;
};

export type Payment = {
  id: string;
  amountPaise: number;
  currency: string;
  note?: string;
  origin: string;
  state: string;
  createdAt: string;
  completedAt?: string;
  failureReason?: string;
  senderName: string;
  senderPaymentIdentifier: string;
  receiverName: string;
  receiverPaymentIdentifier: string;
  direction: "SENT" | "RECEIVED";
  sourceBankName?: string;
  sourceBankCode?: string;
  destinationBankName?: string;
  destinationBankCode?: string;
  durationMs?: number;
};

export type MerchantReceiveInfo = {
  paymentIdentifier: string;
  accountNumber: string;
  accountStatus: string;
};

export type View = "home" | "pay" | "transactions" | "details";
export type MerchantView = "m-home" | "m-receive" | "m-incoming" | "m-settlement" | "m-search";
export type ConsoleView =
  | "c-overview"
  | "c-health"
  | "c-routing"
  | "c-reconciliation"
  | "c-merkle"
  | "c-integrity"
  | "c-chaos"
  | "c-activity";

export type HealthSnapshot = {
  targetId: string;
  score: number;
  availabilityScore: number;
  successScore: number;
  latencyPenalty: number;
  timeoutPenalty: number;
  sampleCount: number;
  windowStartedAt: string;
  computedAt: string;
};

export type HealthSample = {
  TargetID: string;
  SampledAt: string;
  Available: boolean;
  Latency: number;
  Outcome: "SUCCESS" | "FAILURE" | "TIMEOUT";
  CorrelationID: string;
};

export type CircuitTargetSnapshot = {
  executionTargetId: string;
  state: "CLOSED" | "OPEN" | "HALF_OPEN";
  openedAt?: string;
  halfOpenedAt?: string;
  failureCount: number;
  timeoutCount: number;
  consecutiveSuccesses: number;
  activeProbes: number;
  successfulProbes: number;
  restorationStep: number;
  maxRestorationSteps: number;
  restorationProgress: number;
  lastEvaluatedAt: string;
};

export type CircuitTransitionEvent = {
  id?: number;
  executionTargetId: string;
  previousState: "CLOSED" | "OPEN" | "HALF_OPEN";
  newState: "CLOSED" | "OPEN" | "HALF_OPEN";
  reason: string;
  transitionedAt: string;
  failureCount: number;
  timeoutCount: number;
  consecutiveSuccesses: number;
  activeProbes: number;
  successfulProbes: number;
  restorationStep: number;
  cooldownDurationMs: number;
  rollingWindowMs: number;
  details?: Record<string, unknown>;
  eventType: string;
};

export type ChaosScenario = {
  id: string;
  scenarioId: string;
  type: string;
  targetId: string;
  parameters: {
    durationMs?: number;
    latencyMs?: number;
    dropCount?: number;
    dropRate?: number;
    errorMessage?: string;
  };
  startedAt: string;
  expiresAt: string;
  stoppedAt?: string;
  active: boolean;
  mode: string;
  createdBy: string;
  stoppedBy?: string;
  createdAt: string;
  updatedAt: string;
};
