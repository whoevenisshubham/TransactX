import React, { useEffect, useRef, useState } from "react";
import { api } from "../../api";
import { Button, InlineError, PageHeader, Skeleton } from "../../components";
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

      <div className="console-card">
        <h3>Executed route selections · last 24 hours</h3>
        <Button variant="secondary" onClick={loadDistribution}>Refresh distribution</Button>
        {distributionError && <InlineError message={distributionError} />}
        {!distributionError && distribution.length === 0 && <p>No route decisions recorded in this window.</p>}
        {distribution.map((item) => (
          <div className="console-spec-item" key={item.targetId}>
            <span>{item.targetId}</span>
            <span style={{ minWidth: "40%" }}>
              <span
                style={{
                  display: "inline-block",
                  height: ".6rem",
                  background: "var(--accent)",
                  width: `${Math.round((item.payments / Math.max(1, ...distribution.map((x) => x.payments))) * 100)}%`,
                  marginRight: ".5rem",
                }}
              />
              {item.payments} payments
            </span>
          </div>
        ))}
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
          <div className="console-tabs">
            {targetKeys.map((k) => (
              <button
                key={k}
                type="button"
                className={`console-tab ${selectedTarget === k ? "is-active" : ""}`}
                onClick={() => setSelectedTarget(k)}
              >
                Target: {k}
              </button>
            ))}
          </div>

          {currentCircuit && (
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
