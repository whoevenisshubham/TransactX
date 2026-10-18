import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";
import { Button, EmptyState, InlineError, PageHeader } from "../components";
import type { HealthSample, HealthSnapshot } from "../types";
import { deriveOperationalTargets } from "./adminTargets";
import { HealthObservationCard } from "./HealthObservationCard";

export function AdminHealthPage({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [targets, setTargets] = useState<string[]>([]);
  const [target, setTarget] = useState("");
  const [snapshot, setSnapshot] = useState<HealthSnapshot | null>(null);
  const [sample, setSample] = useState<HealthSample | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [sampling, setSampling] = useState(false);
  const generation = useRef(0);
  const load = useCallback(async (selected: string) => {
    const current = ++generation.current;
    setLoading(true); setError("");
    try {
      const available = deriveOperationalTargets(await api.opsCircuits(token));
      if (current !== generation.current) return;
      setTargets(available);
      const next = available.includes(selected) ? selected : available[0] ?? "";
      setTarget(next);
      const observation = next ? await api.opsHealthSnapshot(next,token) : null;
      if (current === generation.current) setSnapshot(observation);
    } catch (failure) { if (current === generation.current) { setSnapshot(null); setError(failure instanceof Error ? failure.message : "Health observation unavailable."); } }
    finally { if (current === generation.current) setLoading(false); }
  },[token]);
  useEffect(() => { void load(""); return () => { generation.current++; }; },[load]);
  useEffect(() => {
    if (!liveRevision || document.hidden) return;
    const timer=window.setTimeout(() => void load(target),350);
    return () => window.clearTimeout(timer);
  },[liveRevision,load,target]);
  async function select(value: string) {
    const current = ++generation.current;
    setTarget(value); setSample(null); setSnapshot(null); setLoading(true); setError("");
    try { const observation = await api.opsHealthSnapshot(value,token); if (current === generation.current) setSnapshot(observation); }
    catch (failure) { if (current === generation.current) setError(failure instanceof Error ? failure.message : "Health observation unavailable."); }
    finally { if (current === generation.current) setLoading(false); }
  }
  async function probe() {
    if (!target || sampling) return;
    setSampling(true); setError(""); setSample(null);
    try { setSample(await api.opsHealthSample(target,token)); setSnapshot(await api.opsHealthSnapshot(target,token)); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "Probe result unavailable. No bank-health conclusion can be made."); }
    finally { setSampling(false); }
  }
  return <div className="ops-page admin-health"><PageHeader eyebrow="Monitor / Source observations" title="Bank health" description="Inspect measured rolling observations for an execution target. Probes observe reachability; they do not settle a payment." action={<Button variant="secondary" disabled={loading} onClick={() => void load(target)}>Refresh observations</Button>} /><div className="health-target-toolbar"><label>Execution target<select aria-label="Execution target" value={target} disabled={!targets.length || loading || sampling} onChange={event => void select(event.target.value)}>{!targets.length && <option value="">No target available</option>}{targets.map(id => <option value={id} key={id}>{id}</option>)}</select></label><Button disabled={sampling || loading || !target} onClick={() => void probe()}>{sampling ? "Collecting probe…" : "Collect one probe"}</Button></div>{error && <InlineError message={error} />}{loading && <p role="status">Collecting the selected observation…</p>}{!loading && targets.length === 0 && !error && <EmptyState title="No execution targets returned" description="No health verdict can be inferred. Refresh when routing observations are available." />}{target && <div className="admin-health-layout"><HealthObservationCard targetId={target} snapshot={snapshot} /><section className="health-interpretation"><p className="eyebrow">How to read this</p><h2>A score needs observations.</h2><p>Sample count, source time and the measured window matter alongside the score. An unavailable response is not evidence that a bank is down; no samples means the score has not been evaluated here.</p><p>Routing decisions remain with the backend controller. Stream connectivity is independent of participant health.</p>{snapshot && <dl><div><dt>Window started</dt><dd>{new Date(snapshot.windowStartedAt).toLocaleString()}</dd></div><div><dt>Computed</dt><dd>{new Date(snapshot.computedAt).toLocaleString()}</dd></div></dl>}</section></div>}{sample && <section className="health-probe-result"><div><p className="eyebrow">Latest requested probe</p><h2>{sample.Outcome.replaceAll("_"," ").toLowerCase()}</h2></div><dl><div><dt>Target</dt><dd>{sample.TargetID}</dd></div><div><dt>Observed</dt><dd>{new Date(sample.SampledAt).toLocaleString()}</dd></div><div><dt>Latency</dt><dd>{(sample.Latency/1_000_000).toFixed(1)} ms</dd></div></dl><details><summary>Probe reference</summary><span className="ops-mono">{sample.CorrelationID}</span></details></section>}</div>;
}
