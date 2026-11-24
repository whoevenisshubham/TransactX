import {
  type ChaosFaultType,
  SUPPORTED_CHAOS_TYPES,
} from "./types";

export { SUPPORTED_CHAOS_TYPES, type ChaosFaultType };

export function buildChaosPayload(
  scenarioId: string,
  targetId: string,
  faultType: ChaosFaultType,
  options: { durationMs?: number; latencyMs?: number; dropRate?: number }
): {
  scenarioId: string;
  targetId: string;
  type: ChaosFaultType;
  parameters: Record<string, unknown>;
} {
  const params: Record<string, unknown> = {
    durationMs: options.durationMs ?? 30000,
  };
  if (faultType === "LATENCY") {
    params.latencyMs = options.latencyMs ?? 1500;
  } else if (faultType === "TRANSIENT_DROP") {
    params.dropRate = options.dropRate ?? 0.5;
  }
  return {
    scenarioId: scenarioId.trim(),
    targetId: targetId.trim(),
    type: faultType,
    parameters: params,
  };
}
