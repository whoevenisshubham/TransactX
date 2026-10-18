import type { CircuitTargetSnapshot } from "../types";

export function deriveOperationalTargets(
  circuitMap: Record<string, CircuitTargetSnapshot> | null | undefined
): string[] {
  if (!circuitMap || typeof circuitMap !== "object") return [];
  const targets = new Set<string>();
  for (const [key, snapshot] of Object.entries(circuitMap)) {
    const k = key.trim();
    if (k) targets.add(k);
    if (snapshot?.executionTargetId?.trim()) {
      targets.add(snapshot.executionTargetId.trim());
    }
  }
  return Array.from(targets).sort();
}

