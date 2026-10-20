import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";
import { Button, EmptyState, PageHeader } from "../components";
import type { CircuitTargetSnapshot, ConsoleView, HealthSnapshot } from "../types";
import { deriveOperationalTargets } from "./adminTargets";
import { HealthObservationCard } from "./HealthObservationCard";

type Reachability = "not_collected" | "responding" | "reported_error" | "unavailable";
const reachabilityCopy: Record<Reachability, string> = { not_collected: "Not collected", responding: "Responding", reported_error: "Reported error", unavailable: "Unavailable" };

export function AdminOverviewPage({ token, onNavigate, liveRevision }: { token: string; onNavigate: (view: ConsoleView) => void; liveRevision: number }) {
  const [circuits, setCircuits] = useState<Record<string, CircuitTargetSnapshot> | null>(null);
  const [observations, setObservations] = useState<Record<string, HealthSnapshot | null>>({});
  const [apiState, setApiState] = useState<Reachability>("not_collected");
  const [dbState, setDbState] = useState<Reachability>("not_collected");
  const [faultCount, setFaultCount] = useState<number | null>(null);
  const [limits, setLimits] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [fetchedAt, setFetchedAt] = useState<string | null>(null);
  const generation = useRef(0);
  const refresh = useCallback(async () => {
    const current = ++generation.current;
    setLoading(true);
    const unavailable: string[] = [];
    try {
      const [routing, faults, application, database] = await Promise.allSettled([
        api.opsCircuits(token), api.opsChaosScenarios(token, true), api.opsGlobalHealth(), api.opsDbHealth(),
      ]);
      if (current !== generation.current) return;
      setApiState(application.status === "fulfilled" ? application.value.status === "ok" ? "responding" : "reported_error" : "unavailable");
      setDbState(database.status === "fulfilled" ? database.value.status === "ok" ? "responding" : "reported_error" : "unavailable");
      if (faults.status === "fulfilled") setFaultCount(faults.value.length);
      else { setFaultCount(null); unavailable.push("Controlled-fault observations are unavailable or disabled."); }
      if (routing.status === "fulfilled") {
        setCircuits(routing.value);
        const targets = deriveOperationalTargets(routing.value);
        const sampled = await Promise.allSettled(targets.map(target => api.opsHealthSnapshot(target, token)));
        if (current !== generation.current) return;
        setObservations(Object.fromEntries(targets.map((target, index) => {
          const value = sampled[index];
          return [target, value.status === "fulfilled" ? value.value : null];
        })));
      } else unavailable.push("Routing observations could not be refreshed. Any displayed circuits are retained from the previous successful fetch.");
      setFetchedAt(new Date().toISOString());
      setLimits(unavailable);
    } finally { if (current === generation.current) setLoading(false); }
  }, [token]);
  useEffect(() => { void refresh(); return () => { generation.current++; }; }, [refresh]);
  useEffect(() => {
    if (!liveRevision || document.hidden) return;
    const timer = window.setTimeout(() => void refresh(), 350);
    return () => window.clearTimeout(timer);
  }, [liveRevision, refresh]);
  const targets = circuits ? deriveOperationalTargets(circuits) : null;
  const openCircuits = circuits ? Object.values(circuits).filter(circuit => circuit.state === "OPEN").length : null;
  return <div className="ops-page admin-overview"><PageHeader eyebrow="Monitor / Private operator" title="Network overview" description="Inspect recorded observations across the simulated network. Each source can be unavailable independently." action={<Button variant="secondary" disabled={loading} onClick={() => void refresh()}>{loading ? "Refreshing…" : "Refresh observations"}</Button>} />
    {fetchedAt && <p className="ops-muted">Last fetch finished {new Date(fetchedAt).toLocaleString()} (device timezone). Source timestamps appear with the observations.</p>}
    {limits.map(message => <p className="ops-banner ops-banner-warning" key={message}>{message}</p>)}
    <section className="admin-network-summary" aria-label="Collected network observations"><article><span>Execution targets</span><strong>{targets?.length ?? "—"}</strong><p>{targets === null ? "Observation unavailable" : "Discovered from the routing response"}</p></article><article><span>Open circuits</span><strong>{openCircuits ?? "—"}</strong><p>{openCircuits === null ? "Not collected" : "From the displayed circuit snapshot"}</p></article><article><span>Active controlled faults</span><strong>{faultCount ?? "—"}</strong><p>{faultCount === null ? "Unavailable or disabled" : "Reported by the fault controller"}</p></article></section>
    <section className="admin-source-observations" aria-label="Service reachability"><div><span>API health endpoint</span><strong>{reachabilityCopy[apiState]}</strong></div><div><span>Database health endpoint</span><strong>{reachabilityCopy[dbState]}</strong></div><p>Reachability checks and event-stream connectivity do not establish a payment outcome or a bank’s financial state.</p></section>
    <div className="section-heading"><div><p className="eyebrow">Target observations</p><h2>Health with its context.</h2></div><Button variant="secondary" onClick={() => onNavigate("c-health")}>Inspect bank health</Button></div>
    {loading && targets === null && <p role="status">Collecting network observations…</p>}
    {targets?.length === 0 && <EmptyState title="No execution targets returned" description="No bank health verdict can be inferred from an empty routing response." />}
    {targets && targets.length > 0 && <div className="health-observation-grid">{targets.map(target => <HealthObservationCard key={target} targetId={target} snapshot={observations[target] ?? null} />)}</div>}
    <section className="admin-inspection-links"><article><h2>Routing & recovery</h2><p>Inspect circuit state, route observations and the original payment journey.</p><Button variant="secondary" onClick={() => onNavigate("c-routing")}>Inspect routing</Button><Button variant="quiet" onClick={() => onNavigate("c-worklist")}>Open worklist</Button></article><article><h2>Verify the records</h2><p>Compare participant and canonical records, then inspect their proof and integrity evidence.</p><Button variant="secondary" onClick={() => onNavigate("c-reconciliation")}>Open reconciliation</Button><Button variant="quiet" onClick={() => onNavigate("c-integrity")}>Inspect integrity</Button></article></section>
  </div>;
}
