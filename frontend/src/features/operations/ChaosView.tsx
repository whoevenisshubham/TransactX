import React, { useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "../../api";
import { Button, InlineError, PageHeader, Skeleton } from "../../components";
import { DotSentinel, useAudioEngine } from "../../components/effects";
import type { ChaosScenario } from "./types";
import { ConsoleIcon } from "./ConsoleIcon";
import { formatIso } from "./format";
import { buildChaosPayload, type ChaosFaultType } from "./chaos";
import { deriveOperationalTargets } from "../../ops/adminTargets";

export function ChaosView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [scenarios, setScenarios] = useState<ChaosScenario[]>([]);
  const [circuitTargets, setCircuitTargets] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [feedback, setFeedback] = useState("");
  const { play } = useAudioEngine();

  const [scenarioId, setScenarioId] = useState(`chaos-${Date.now().toString(36)}`);
  const [targetId, setTargetId] = useState("");
  const [faultType, setFaultType] = useState<ChaosFaultType>("BANK_OUTAGE");
  const [durationMs, setDurationMs] = useState(30000);
  const [latencyMs, setLatencyMs] = useState(1500);
  const [dropRate, setDropRate] = useState(0.5);

  function loadData() {
    setLoading(true);
    setError("");
    Promise.all([
      api.opsChaosScenarios(token),
      api.opsCircuits(token).catch(() => ({})),
    ])
      .then(([loadedScenarios, circuitMap]) => {
        setScenarios(loadedScenarios);
        const derived = deriveOperationalTargets(circuitMap);
        setCircuitTargets(derived);
        if (derived.length > 0) {
          setTargetId((prev) => (derived.includes(prev) ? prev : derived[0]));
        } else {
          setTargetId("");
        }
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to load chaos status."))
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    loadData();
  }, [token, liveRevision]);

  function handleStart(e: React.FormEvent) {
    e.preventDefault();
    if (!targetId || circuitTargets.length === 0) {
      setError("Cannot submit scenario: no valid operational target available.");
      return;
    }
    setSubmitting(true);
    setError("");
    setFeedback("");

    const payload = buildChaosPayload(scenarioId, targetId, faultType, {
      durationMs,
      latencyMs,
      dropRate,
    });

    api.opsChaosStart(payload, token)
      .then((created) => {
        setFeedback(`Scenario ${created.scenarioId} engaged on target ${created.targetId}.`);
        play("error");
        toast.warning(`Fault injected: ${created.scenarioId} (${created.targetId})`);
        setScenarioId(`chaos-${Date.now().toString(36)}`);
        loadData();
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to start scenario."))
      .finally(() => setSubmitting(false));
  }

  function handleStop(id: string) {
    setError("");
    setFeedback("");
    api.opsChaosStop(id, token)
      .then(() => {
        setFeedback(`Scenario ${id} stopped.`);
        play("tab");
        toast.success(`Scenario ${id} stopped`);
        loadData();
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to stop scenario."));
  }

  function handleReset() {
    setError("");
    setFeedback("");
    api.opsChaosReset(undefined, token)
      .then(() => {
        setFeedback("All active chaos faults reset.");
        play("success");
        toast.success("All chaos faults cleared");
        loadData();
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to reset chaos."));
  }

  async function runCorruptionFixture() {
    setError("");
    setFeedback("");
    try {
      const result = await api.opsCorruptionFixture(token);
      setFeedback(
        `Isolated simulation: baseline ${result.baselineIntegrity.status}; after fixed ledger mutation ${result.corruptedIntegrity.status}; Merkle root mismatch ${result.merkleRootMismatch}; reconciliation discrepancies ${result.reconciliationDiscrepancies}.`
      );
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Corruption fixture unavailable. Enable TX_SIMULATION_MODE=true on the API."
      );
    }
  }

  const activeList = scenarios.filter((s) => s.active);
  const inactiveList = scenarios.filter((s) => !s.active);

  return (
    <>
      <PageHeader
        eyebrow="Fault Injection"
        title="Chaos Simulation Controller"
        description="Induce synthetic communication faults, latency, and bank outages to observe resilience in action."
        action={
          <div style={{ display: "flex", gap: ".5rem" }}>
            <Button variant="secondary" onClick={handleReset}>
              Reset All Faults
            </Button>
            <Button variant="secondary" onClick={loadData}>
              <ConsoleIcon name="refresh" size={14} />
              Refresh
            </Button>
          </div>
        }
      />

      {error && <InlineError message={error} />}
      {feedback && (
        <div className="console-card" style={{ borderColor: "var(--accent)", color: "var(--accent)" }}>
          {feedback}
        </div>
      )}

      <div className="console-card">
        <h3>Controlled ledger corruption fixture</h3>
        <p>
          Isolated simulation only. The fixed test record is discarded after verification; production ledger data is
          never changed. Requires TX_SIMULATION_MODE=true on the API.
        </p>
        <Button variant="secondary" onClick={runCorruptionFixture}>
          Run corruption detection
        </Button>
      </div>

      {circuitTargets.length === 0 && !loading && (
        <div className="console-notice-box" style={{ marginBottom: "1.25rem", padding: "1rem" }}>
          <span className="console-notice-tag">TARGET REGISTRATION REQUIRED</span>
          <p style={{ margin: 0 }}>
            No execution targets are registered with the circuit breaker (<code>GET /api/ops/circuit</code> returned 0
            targets). Scenario submission is disabled until operational targets are available.
          </p>
        </div>
      )}

      <form className="console-form-inline" onSubmit={handleStart}>
        <div className="console-form-group">
          <label className="console-form-label" htmlFor="chaos-id">Scenario ID</label>
          <input
            id="chaos-id"
            className="console-input"
            value={scenarioId}
            onChange={(e) => setScenarioId(e.target.value)}
            required
          />
        </div>

        <div className="console-form-group">
          <label className="console-form-label" htmlFor="chaos-target">Target</label>
          <select
            id="chaos-target"
            className="console-select"
            value={targetId}
            onChange={(e) => setTargetId(e.target.value)}
            disabled={circuitTargets.length === 0}
            required
          >
            {circuitTargets.length === 0 ? (
              <option value="" disabled>No operational targets</option>
            ) : (
              circuitTargets.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))
            )}
          </select>
        </div>

        <div className="console-form-group">
          <label className="console-form-label" htmlFor="chaos-type">Fault Type</label>
          <select
            id="chaos-type"
            className="console-select"
            value={faultType}
            onChange={(e) => setFaultType(e.target.value as ChaosFaultType)}
          >
            <option value="BANK_OUTAGE">BANK_OUTAGE (Total Outage)</option>
            <option value="LATENCY">LATENCY (Delay Injection)</option>
            <option value="TRANSIENT_DROP">TRANSIENT_DROP (Packet Drops)</option>
            <option value="TEMPORARY_PARTITION">TEMPORARY_PARTITION (Network Partition)</option>
          </select>
        </div>

        <div className="console-form-group" style={{ minWidth: 100 }}>
          <label className="console-form-label" htmlFor="chaos-duration">Duration (ms)</label>
          <input
            id="chaos-duration"
            type="number"
            className="console-input"
            value={durationMs}
            onChange={(e) => setDurationMs(Number(e.target.value))}
            min={1000}
            max={600000}
            step={1000}
            required
          />
        </div>

        {faultType === "LATENCY" && (
          <div className="console-form-group" style={{ minWidth: 100 }}>
            <label className="console-form-label" htmlFor="chaos-latency">Latency (ms)</label>
            <input
              id="chaos-latency"
              type="number"
              className="console-input"
              value={latencyMs}
              onChange={(e) => setLatencyMs(Number(e.target.value))}
              min={1}
              max={30000}
              required
            />
          </div>
        )}

        {faultType === "TRANSIENT_DROP" && (
          <div className="console-form-group" style={{ minWidth: 100 }}>
            <label className="console-form-label" htmlFor="chaos-droprate">Drop Rate</label>
            <input
              id="chaos-droprate"
              type="number"
              className="console-input"
              value={dropRate}
              onChange={(e) => setDropRate(Number(e.target.value))}
              min={0}
              max={1}
              step={0.1}
              required
            />
          </div>
        )}

        <Button type="submit" disabled={submitting || circuitTargets.length === 0 || !targetId}>
          {submitting ? "Engaging…" : "Engage Scenario"}
        </Button>
      </form>

      {loading ? (
        <ConsoleSkeleton />
      ) : (
        <>
          <section className="section-block">
            <div className="section-heading">
              <div>
                <p className="eyebrow flex items-center gap-2">
                  <DotSentinel state={activeList.length > 0 ? "error" : "idle"} />
                  Active Scenarios
                </p>
                <h2>Currently Running Injections ({activeList.length})</h2>
              </div>
            </div>
            {activeList.length === 0 ? (
              <div className="console-card">
                <p className="console-meta-text">
                  No active controlled-fault scenarios reported. This list does not establish bank health.
                </p>
              </div>
            ) : (
              <div className="console-table-wrap" tabIndex={0} role="region" aria-label="Operational evidence table">
                <table className="console-table">
                  <thead>
                    <tr>
                      <th>Scenario ID</th>
                      <th>Target</th>
                      <th>Type</th>
                      <th>Parameters</th>
                      <th>Started</th>
                      <th>Expires</th>
                      <th>Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {activeList.map((s) => (
                      <tr key={s.id}>
                        <td className="mono" style={{ fontWeight: 600 }}>{s.scenarioId}</td>
                        <td className="mono">{s.targetId}</td>
                        <td><span className="console-pill console-pill-warning">{s.type}</span></td>
                        <td className="mono" style={{ fontSize: ".72rem" }}>
                          {s.parameters.latencyMs ? `lat: ${s.parameters.latencyMs}ms ` : ""}
                          {s.parameters.dropRate ? `drop: ${s.parameters.dropRate * 100}% ` : ""}
                          dur: {s.parameters.durationMs}ms
                        </td>
                        <td className="mono">{formatIso(s.startedAt)}</td>
                        <td className="mono">{formatIso(s.expiresAt)}</td>
                        <td>
                          <button
                            type="button"
                            className="button button-sm button-secondary"
                            onClick={() => handleStop(s.scenarioId)}
                          >
                            Stop
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          {inactiveList.length > 0 && (
            <section className="section-block">
              <div className="section-heading">
                <div>
                  <p className="eyebrow">Audit Log</p>
                  <h2>Historical Scenarios</h2>
                </div>
              </div>
              <div className="console-table-wrap" tabIndex={0} role="region" aria-label="Operational evidence table">
                <table className="console-table">
                  <thead>
                    <tr>
                      <th>Scenario ID</th>
                      <th>Target</th>
                      <th>Type</th>
                      <th>Started</th>
                      <th>Stopped / Expired</th>
                      <th>Created By</th>
                    </tr>
                  </thead>
                  <tbody>
                    {inactiveList.slice(0, 10).map((s) => (
                      <tr key={s.id}>
                        <td className="mono">{s.scenarioId}</td>
                        <td className="mono">{s.targetId}</td>
                        <td><span className="console-pill console-pill-neutral">{s.type}</span></td>
                        <td className="mono">{formatIso(s.startedAt)}</td>
                        <td className="mono">{s.stoppedAt ? formatIso(s.stoppedAt) : formatIso(s.expiresAt)}</td>
                        <td className="mono" style={{ fontSize: ".72rem" }}>{s.createdBy}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          )}
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
