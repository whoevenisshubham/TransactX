import data from "./scenarios.v1.json";
import type { Payment } from "../types";

export type ScenarioEvent = { id: string; at: string; source: string; title: string; detail: string };
export type ScenarioRecord = { payment: Payment; timeline: ScenarioEvent[] };
export type Scenario = { id: string; title: string; category: string; summary: string; boundary: string; answer: string; schemaVersion: number; source: "illustrative"; records: ScenarioRecord[] };
export const showcaseLibrary = data as { schemaVersion: number; source: "illustrative"; scenarioDate: string; stories: Scenario[] };

export function scenarioTime(value: string) {
  return new Intl.DateTimeFormat("en-GB", { timeZone: "UTC", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" }).format(new Date(value));
}
