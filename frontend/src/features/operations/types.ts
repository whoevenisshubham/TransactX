import type {
  ActivityEvent,
  ChaosScenario,
  CircuitTargetSnapshot,
  CircuitTransitionEvent,
  ConsoleView,
  HealthSample,
  HealthSnapshot,
  CheckStatus,
  CheckViolation,
  IntegrityCheckResult,
  IntegrityProof,
  IntegrityRunResult,
  IntegrityRunStatus,
  IntegrityRunSummary,
  IntegrityStatusResponse,
  IntegrityVerifyProofResult,
  MerkleTreeChild,
  MerkleTreeRoot,
  User,
} from "../../types";

export type {
  ActivityEvent,
  ChaosScenario,
  CircuitTargetSnapshot,
  CircuitTransitionEvent,
  ConsoleView,
  HealthSample,
  HealthSnapshot,
  CheckStatus,
  CheckViolation,
  IntegrityCheckResult,
  IntegrityProof,
  IntegrityRunResult,
  IntegrityRunStatus,
  IntegrityRunSummary,
  IntegrityStatusResponse,
  IntegrityVerifyProofResult,
  MerkleTreeChild,
  MerkleTreeRoot,
  User,
};

export type ChaosFaultType =
  | "BANK_OUTAGE"
  | "LATENCY"
  | "TRANSIENT_DROP"
  | "TEMPORARY_PARTITION";

export const SUPPORTED_CHAOS_TYPES: readonly ChaosFaultType[] = [
  "BANK_OUTAGE",
  "LATENCY",
  "TRANSIENT_DROP",
  "TEMPORARY_PARTITION",
] as const;

export type ConsoleIconName =
  | "overview"
  | "health"
  | "routing"
  | "reconciliation"
  | "merkle"
  | "integrity"
  | "chaos"
  | "activity"
  | "pulse"
  | "refresh";

export interface NavigationLink {
  view: ConsoleView;
  label: string;
  icon: ConsoleIconName;
}

export interface NavigationGroup {
  title: string;
  admin: boolean;
  links: NavigationLink[];
}
