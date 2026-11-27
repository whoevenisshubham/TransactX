import React, { useEffect, useRef, useState } from "react";
import { motion } from "motion/react";
import { api } from "../../api";
import { Button, InlineError, PageHeader, Skeleton } from "../../components";
import { DotSentinel, useAudioEngine } from "../../components/effects";
import type { CircuitTargetSnapshot, CircuitTransitionEvent } from "./types";
import { ConsoleIcon } from "./ConsoleIcon";
import { formatIso } from "./format";

export function RoutingView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [distribution, setDistribution] = useState<{ targetId: string; payments: number }[]>([]);
  const [distributionError, setDistributionError] = useState("");
  const [circuits, setCircuits] = useState<Record<string, CircuitTargetSnapshot>>({});
  const [selectedTarget, setSelectedTarget] = useState<string>("BANK-A");
  const [events, setEvents] = useState<CircuitTransitionEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [eventsError, setEventsError] = useState("");
  const eventGeneration = useRef(0);
  const circuitGeneration = useRef(0);
  const distributionGeneration = useRef(0);
  const eventTarget = useRef("");
  const [error, setError] = useState("");
  const { play } = useAudioEngine();

  function loadDistribution() {
    const generation = ++distributionGeneration.current;
    api.opsRoutingDistribution(token)
      .then((data) => {
        if (generation === distributionGeneration.current) {
          setDistribution(data.items);
          setDistributionError("");
        }
      })
      .catch((err) => {
        if (generation === distributionGeneration.current) {
          setDistributionError(err instanceof Error ? err.message : "Could not load route selections.");
        }
      });
  }

  function loadCircuits() {
    const generation = ++circuitGeneration.current;
    setLoading(true);
    setError("");
    api.opsCircuits(token)
      .then((map) => {
        if (generation === circuitGeneration.current) {
          setCircuits(map);
          const keys = Object.keys(map);
          if (keys.length > 0 && !keys.includes(selectedTarget)) {
            setSelectedTarget(keys[0]);
          }
        }
      })
      .catch((err) => {
        if (generation === circuitGeneration.current) {
          setError(err instanceof Error ? err.message : "Failed to load circuit states.");
        }
      })
      .finally(() => {
        if (generation === circuitGeneration.current) setLoading(false);
      });
  }

  function loadEvents(target: string) {
    const generation = ++eventGeneration.current;
    if (eventTarget.current !== target) setEvents([]);
    eventTarget.current = target;
    setEventsLoading(true);
    setEventsError("");
    api.opsCircuitEvents(target, token)
      .then((values) => {
        if (generation === eventGeneration.current) setEvents(values);
      })
      .catch((failure) => {
        if (generation === eventGeneration.current) {
          setEventsError(failure instanceof Error ? failure.message : "Circuit history unavailable.");
        }
      })
      .finally(() => {
        if (generation === eventGeneration.current) setEventsLoading(false);
      });
  }

  useEffect(() => {
    loadDistribution();
  }, [token, liveRevision]);

  useEffect(() => {
    loadCircuits();
  }, [token, liveRevision]);

  useEffect(() => {
    if (selectedTarget) {
      loadEvents(selectedTarget);
    }
  }, [selectedTarget, token, liveRevision]);

  const targetKeys = Object.keys(circuits);
  const currentCircuit = circuits[selectedTarget];

  return (
    <>
      <PageHeader
        eyebrow="Adaptive Routing"
        title="Target Circuit Controls"
        description="Circuit breaker state machine enforcing target eligibility, cooldowns, and progressive recovery probes."
        action={
          <Button variant="secondary" onClick={loadCircuits}>
            <ConsoleIcon name="refresh" size={14} />
            Refresh Circuits
          </Button>
        }
      />

      <div className="console-card space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="!m-0">Executed route selections · last 24 hours</h3>
          <Button variant="secondary" onClick={loadDistribution}>Refresh distribution</Button>
        </div>
        {distributionError && <InlineError message={distributionError} />}
        {!distributionError && distribution.length === 0 && <p className="ops-muted">No route decisions recorded in this window.</p>}
        {distribution.map((item) => {
          const pct = Math.round((item.payments / Math.max(1, ...distribution.map((x) => x.payments))) * 100);
          return (
            <div className="console-spec-item" key={item.targetId}>
              <span className="ops-mono font-semibold text-[var(--text-primary)]">{item.targetId}</span>
              <span className="flex items-center gap-3 min-w-[220px] flex-1 max-w-md justify-end">
                <span className="flex-1 h-2.5 rounded-full bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)] overflow-hidden">
                  <span
                    className="block h-full rounded-full bg-[var(--action-accent-bg)]"
                    style={{ width: `${pct}%` }}
                  />
                </span>
                <span className="ops-mono tabular-nums text-xs text-[var(--text-secondary)] shrink-0">
                  {item.payments} payments
                </span>
              </span>
            </div>
          );
        })}
      </div>

      {error && <InlineError message={error} />}

      {loading ? (
        <ConsoleSkeleton />
      ) : targetKeys.length === 0 ? (
        <div className="console-card">
          <span className="console-card-kicker">Circuit Breakers</span>
          <p className="console-meta-text">No execution targets are registered with the circuit breaker.</p>
        </div>
      ) : (
        <>
          <div className="console-tabs" role="tablist" aria-label="Execution target selector">
            {targetKeys.map((k) => {
              const active = selectedTarget === k;
              const targetState = circuits[k]?.state;
              return (
                <button
                  key={k}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  className={`console-tab relative overflow-hidden inline-flex items-center gap-2 ${active ? "is-active" : ""}`}
                  onClick={() => {
                    play("tab");
                    setSelectedTarget(k);
                  }}
                >
                  {active && (
                    <motion.span
                      layoutId="routing-target-pill"
                      transition={{ type: "spring", stiffness: 380, damping: 32 }}
                      className="absolute inset-0 rounded-[inherit] bg-[var(--bg-surface-raised)] border border-[var(--border-active)]"
                      aria-hidden="true"
                    />
                  )}
                  <span className="relative z-10 inline-flex items-center gap-2">
                    <DotSentinel
                      state={
                        targetState === "CLOSED"
                          ? "success"
                          : targetState === "OPEN"
                          ? "error"
                          : "warning"
                      }
                    />
                    Target: {k}
                  </span>
                </button>
              );
            })}
          </div>

          {currentCircuit && (
            <>
              {/* Interactive Circuit-Breaker State Machine Visualizer */}
              <div className="rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-4 shadow-[var(--shadow-subtle)] mb-4">
                <div className="flex flex-wrap items-center justify-between gap-2 mb-3">
                  <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-[var(--text-secondary)]">
                    Circuit state machine · {selectedTarget}
                  </span>
                  <span className="text-[11px] font-mono text-[var(--text-tertiary)]">
                    Recovery progress {(currentCircuit.restorationProgress * 100).toFixed(0)}%
                  </span>
                </div>
                <div className="grid grid-cols-3 gap-2 sm:gap-3 items-center text-center">
                  {(
                    [
                      {
                        state: "CLOSED",
                        title: "CLOSED",
                        desc: "Full routing eligible",
                        tone: "border-[var(--status-success-border)] bg-[var(--status-success-bg)] text-[var(--status-success-fg)]",
                      },
                      {
                        state: "HALF_OPEN",
                        title: "HALF_OPEN",
                        desc: "Limited canary probes",
                        tone: "border-[var(--status-warning-border)] bg-[var(--status-warning-bg)] text-[var(--status-warning-fg)]",
                      },
                      {
                        state: "OPEN",
                        title: "OPEN",
                        desc: "Fast-fail isolated",
                        tone: "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-[var(--status-danger-fg)]",
                      },
                    ] as const
                  ).map((node) => {
                    const isCurrent = currentCircuit.state === node.state;
                    return (
                      <div
                        key={node.state}
                        className={`rounded-xl border px-2.5 py-2.5 transition-all ${
                          isCurrent
                            ? `${node.tone} ring-2 ring-current/20 shadow-sm`
                            : "border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 text-[var(--text-secondary)]"
                        }`}
                      >
                        <div className="font-mono text-xs font-bold tracking-tight">
                          {node.title}
                        </div>
                        <div className="text-[10px] mt-0.5 hidden sm:block">
                          {node.desc}
                        </div>
                      </div>
                    );
                  })}
                </div>
                <div className="mt-3 h-1.5 w-full rounded-full bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)] overflow-hidden" aria-hidden="true">
                  <motion.div
                    className="h-full rounded-full bg-[var(--action-accent-bg)]"
                    initial={false}
                    animate={{ width: `${Math.max(4, Math.min(100, currentCircuit.restorationProgress * 100))}%` }}
                    transition={{ duration: 0.45, ease: [0.22, 1, 0.36, 1] }}
                  />
                </div>
              </div>

              <section className="console-grid-4">
              <div className="console-card">
                <span className="console-card-kicker">Circuit State</span>
                <span
                  className={`console-pill ${
                    currentCircuit.state === "CLOSED"
                      ? "console-pill-success"
                      : currentCircuit.state === "OPEN"
                      ? "console-pill-error"
                      : "console-pill-warning"
                  }`}
                  style={{ alignSelf: "flex-start", marginTop: ".25rem" }}
                >
                  {currentCircuit.state}
                </span>
                <span className="console-meta-text">
                  {currentCircuit.state === "CLOSED"
                    ? "Normal execution · Eligible"
                    : currentCircuit.state === "OPEN"
                    ? "Trip threshold reached · Ineligible"
                    : "Half-open · Limited probe eligibility"}
                </span>
              </div>

              <div className="console-card">
                <span className="console-card-kicker">Consecutive Successes</span>
                <span className="console-val-large">{currentCircuit.consecutiveSuccesses}</span>
                <span className="console-meta-text">Required to advance state</span>
              </div>

              <div className="console-card">
                <span className="console-card-kicker">Failure & Timeout Counts</span>
                <span className="console-val-large">
                  {currentCircuit.failureCount} / {currentCircuit.timeoutCount}
                </span>
                <span className="console-meta-text">Rolling failure window</span>
              </div>

              <div className="console-card">
                <span className="console-card-kicker">Recovery Progress</span>
                <span className="console-val-large">
                  {(currentCircuit.restorationProgress * 100).toFixed(0)}%
                </span>
                <span className="console-meta-text">
                  Step {currentCircuit.restorationStep} of {currentCircuit.maxRestorationSteps || 1}
                </span>
              </div>
            </section>
            </>
          )}

          <section className="section-block">
            <div className="section-heading">
              <div>
                <p className="eyebrow">Transition History</p>
                <h2>Recent Circuit State Events for {selectedTarget}</h2>
              </div>
              <button type="button" className="text-link" onClick={() => loadEvents(selectedTarget)}>
                Refresh log
              </button>
            </div>
            {eventsError && <InlineError message={eventsError} />}
            {eventsLoading ? (
              <ConsoleSkeleton />
            ) : events.length === 0 ? (
              <div className="console-card">
                <p className="console-meta-text">
                  {eventsError
                    ? "Circuit history is unavailable; an empty display does not establish that no transitions occurred."
                    : `No state transition events were returned for ${selectedTarget}.`}
                </p>
              </div>
            ) : (
              <div className="console-table-wrap" tabIndex={0} role="region" aria-label="Operational evidence table">
                <table className="console-table">
                  <thead>
                    <tr>
                      <th>Time</th>
                      <th>Transition</th>
                      <th>Reason Code</th>
                      <th>Failures</th>
                      <th>Timeouts</th>
                      <th>Step</th>
                    </tr>
                  </thead>
                  <tbody>
                    {events.map((ev, i) => (
                      <tr key={ev.id ?? i}>
                        <td className="mono">{formatIso(ev.transitionedAt)}</td>
                        <td>
                          <span className="console-pill console-pill-neutral" style={{ marginRight: ".35rem" }}>
                            {ev.previousState}
                          </span>
                          →
                          <span
                            className={`console-pill ${
                              ev.newState === "CLOSED"
                                ? "console-pill-success"
                                : ev.newState === "OPEN"
                                ? "console-pill-error"
                                : "console-pill-warning"
                            }`}
                            style={{ marginLeft: ".35rem" }}
                          >
                            {ev.newState}
                          </span>
                        </td>
                        <td className="mono" style={{ fontSize: ".72rem" }}>{ev.reason}</td>
                        <td className="mono">{ev.failureCount}</td>
                        <td className="mono">{ev.timeoutCount}</td>
                        <td className="mono">{ev.restorationStep}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </>
      )}
    </>
  );
}

function ConsoleSkeleton() {
  return (
    <div style={{ display: "grid", gap: "1rem" }}>
      <Skeleton className="skeleton-heading" />
      <div className="console-grid-4">
        <Skeleton className="skeleton-block" />
        <Skeleton className="skeleton-block" />
        <Skeleton className="skeleton-block" />
        <Skeleton className="skeleton-block" />
      </div>
    </div>
  );
}
