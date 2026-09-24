import { useEffect, useState } from "react";
import { api, ApiError } from "./api";
import { Avatar, BrandMark, Button, InlineError, PageHeader, Skeleton } from "./components";
import type {
  ChaosScenario,
  CircuitTargetSnapshot,
  CircuitTransitionEvent,
  ConsoleView,
  HealthSample,
  HealthSnapshot,
  User,
} from "./types";

export function readConsoleView(pathname = typeof window !== "undefined" ? window.location.pathname : "/console"): ConsoleView {
  if (pathname.startsWith("/console/health")) return "c-health";
  if (pathname.startsWith("/console/routing")) return "c-routing";
  if (pathname.startsWith("/console/reconciliation")) return "c-reconciliation";
  if (pathname.startsWith("/console/merkle")) return "c-merkle";
  if (pathname.startsWith("/console/integrity")) return "c-integrity";
  if (pathname.startsWith("/console/chaos")) return "c-chaos";
  if (pathname.startsWith("/console/activity")) return "c-activity";
  return "c-overview";
}

export function consoleViewPath(view: ConsoleView): string {
  switch (view) {
    case "c-health": return "/console/health";
    case "c-routing": return "/console/routing";
    case "c-reconciliation": return "/console/reconciliation";
    case "c-merkle": return "/console/merkle";
    case "c-integrity": return "/console/integrity";
    case "c-chaos": return "/console/chaos";
    case "c-activity": return "/console/activity";
    default: return "/console";
  }
}

// ---------------------------------------------------------------------------
// Operational Target & Chaos Types
// ---------------------------------------------------------------------------

export type ChaosFaultType = "BANK_OUTAGE" | "LATENCY" | "TRANSIENT_DROP" | "TEMPORARY_PARTITION";

export const SUPPORTED_CHAOS_TYPES: readonly ChaosFaultType[] = [
  "BANK_OUTAGE",
  "LATENCY",
  "TRANSIENT_DROP",
  "TEMPORARY_PARTITION",
] as const;

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

// ---------------------------------------------------------------------------
// Shell
// ---------------------------------------------------------------------------

export function ConsoleShell({ token, user, onLogout }: { token: string; user: User; onLogout: () => void }) {
  const [view, setView] = useState<ConsoleView>(readConsoleView());

  useEffect(() => {
    const handle = () => setView(readConsoleView());
    window.addEventListener("popstate", handle);
    return () => window.removeEventListener("popstate", handle);
  }, []);

  function navigate(nextView: ConsoleView) {
    const path = consoleViewPath(nextView);
    window.history.pushState({}, "", path);
    setView(nextView);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  return (
    <div className="product-shell console-shell" data-theme="network-console">
      <ConsoleSidebar view={view} user={user} onNavigate={navigate} onLogout={onLogout} />
      <main className="main-content">
        <ConsoleMobileHeader user={user} onLogout={onLogout} />
        <div className="content-wrap">
          {view === "c-overview" && <ConsoleOverviewView token={token} onNavigate={navigate} />}
          {view === "c-health" && <ConsoleHealthView token={token} />}
          {view === "c-routing" && <ConsoleRoutingView token={token} />}
          {view === "c-reconciliation" && <ConsoleReconciliationView />}
          {view === "c-merkle" && <ConsoleMerkleView />}
          {view === "c-integrity" && <ConsoleIntegrityView />}
          {view === "c-chaos" && <ConsoleChaosView token={token} />}
          {view === "c-activity" && <ConsoleActivityView token={token} />}
        </div>
      </main>
      <ConsoleMobileNav view={view} onNavigate={navigate} />
    </div>
  );
}

function ConsoleSidebar({
  view,
  user,
  onNavigate,
  onLogout,
}: {
  view: ConsoleView;
  user: User;
  onNavigate: (v: ConsoleView) => void;
  onLogout: () => void;
}) {
  return (
    <aside className="sidebar console-sidebar">
      <div className="brand-lockup">
        <BrandMark />
        <span>TransactX</span>
        <span className="console-badge-role">OPS_ADMIN</span>
      </div>
      <div className="sidebar-rule" />
      <nav aria-label="Console navigation">
        <ConsoleNavItem view="c-overview" active={view === "c-overview"} label="Overview" icon="overview" onClick={onNavigate} />
        <ConsoleNavItem view="c-health" active={view === "c-health"} label="Bank Health" icon="health" onClick={onNavigate} />
        <ConsoleNavItem view="c-routing" active={view === "c-routing"} label="Routing" icon="routing" onClick={onNavigate} />
        <ConsoleNavItem view="c-reconciliation" active={view === "c-reconciliation"} label="Reconciliation" icon="reconciliation" onClick={onNavigate} />
        <ConsoleNavItem view="c-merkle" active={view === "c-merkle"} label="Merkle" icon="merkle" onClick={onNavigate} />
        <ConsoleNavItem view="c-integrity" active={view === "c-integrity"} label="Integrity" icon="integrity" onClick={onNavigate} />
        <ConsoleNavItem view="c-chaos" active={view === "c-chaos"} label="Chaos" icon="chaos" onClick={onNavigate} />
        <ConsoleNavItem view="c-activity" active={view === "c-activity"} label="Activity" icon="activity" onClick={onNavigate} />
      </nav>
      <div className="sidebar-bottom">
        <div className="user-chip">
          <Avatar name={user.name} />
          <span>
            <strong>{user.name}</strong>
            <small>{user.paymentIdentifier}</small>
          </span>
        </div>
        <button type="button" className="logout-button" onClick={onLogout}>
          Log out
        </button>
      </div>
    </aside>
  );
}

function ConsoleNavItem({
  view,
  active,
  label,
  icon,
  onClick,
}: {
  view: ConsoleView;
  active: boolean;
  label: string;
  icon: ConsoleIconName;
  onClick: (v: ConsoleView) => void;
}) {
  return (
    <button
      type="button"
      className={`nav-item ${active ? "is-active" : ""}`}
      aria-current={active ? "page" : undefined}
      onClick={() => onClick(view)}
    >
      <ConsoleIcon name={icon} />
      <span>{label}</span>
    </button>
  );
}

function ConsoleMobileHeader({ user, onLogout }: { user: User; onLogout: () => void }) {
  return (
    <header className="mobile-header console-mobile-header">
      <div className="mobile-brand">
        <BrandMark />
        <span>TransactX</span>
        <span className="console-badge-role">OPS_ADMIN</span>
      </div>
      <button type="button" className="mobile-user" onClick={onLogout} aria-label={`Log out ${user.name}`}>
        <Avatar name={user.name} size="sm" />
      </button>
    </header>
  );
}

function ConsoleMobileNav({ view, onNavigate }: { view: ConsoleView; onNavigate: (v: ConsoleView) => void }) {
  return (
    <nav className="mobile-nav console-mobile-nav" aria-label="Mobile network console navigation">
      <button type="button" className={`mobile-nav-item ${view === "c-overview" ? "is-active" : ""}`} aria-current={view === "c-overview" ? "page" : undefined} onClick={() => onNavigate("c-overview")}>
        <ConsoleIcon name="overview" size={15} />
        <span>Overview</span>
      </button>
      <button type="button" className={`mobile-nav-item ${view === "c-health" ? "is-active" : ""}`} aria-current={view === "c-health" ? "page" : undefined} onClick={() => onNavigate("c-health")}>
        <ConsoleIcon name="health" size={15} />
        <span>Health</span>
      </button>
      <button type="button" className={`mobile-nav-item ${view === "c-routing" ? "is-active" : ""}`} aria-current={view === "c-routing" ? "page" : undefined} onClick={() => onNavigate("c-routing")}>
        <ConsoleIcon name="routing" size={15} />
        <span>Routing</span>
      </button>
      <button type="button" className={`mobile-nav-item ${view === "c-reconciliation" ? "is-active" : ""}`} aria-current={view === "c-reconciliation" ? "page" : undefined} onClick={() => onNavigate("c-reconciliation")}>
        <ConsoleIcon name="reconciliation" size={15} />
        <span>Recon</span>
      </button>
      <button type="button" className={`mobile-nav-item ${view === "c-merkle" ? "is-active" : ""}`} aria-current={view === "c-merkle" ? "page" : undefined} onClick={() => onNavigate("c-merkle")}>
        <ConsoleIcon name="merkle" size={15} />
        <span>Merkle</span>
      </button>
      <button type="button" className={`mobile-nav-item ${view === "c-integrity" ? "is-active" : ""}`} aria-current={view === "c-integrity" ? "page" : undefined} onClick={() => onNavigate("c-integrity")}>
        <ConsoleIcon name="integrity" size={15} />
        <span>Integrity</span>
      </button>
      <button type="button" className={`mobile-nav-item ${view === "c-chaos" ? "is-active" : ""}`} aria-current={view === "c-chaos" ? "page" : undefined} onClick={() => onNavigate("c-chaos")}>
        <ConsoleIcon name="chaos" size={15} />
        <span>Chaos</span>
      </button>
      <button type="button" className={`mobile-nav-item ${view === "c-activity" ? "is-active" : ""}`} aria-current={view === "c-activity" ? "page" : undefined} onClick={() => onNavigate("c-activity")}>
        <ConsoleIcon name="activity" size={15} />
        <span>Activity</span>
      </button>
    </nav>
  );
}

// ---------------------------------------------------------------------------
// 1. Overview View
// ---------------------------------------------------------------------------

function ConsoleOverviewView({ token, onNavigate }: { token: string; onNavigate: (v: ConsoleView) => void }) {
  const [circuits, setCircuits] = useState<Record<string, CircuitTargetSnapshot>>({});
  const [chaosScenarios, setChaosScenarios] = useState<ChaosScenario[]>([]);
  const [healthMap, setHealthMap] = useState<Record<string, HealthSnapshot | null>>({});
  const [apiOnline, setApiOnline] = useState(true);
  const [dbOnline, setDbOnline] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  function loadOverview() {
    setLoading(true);
    setError("");
    Promise.all([
      api.opsCircuits(token).catch(() => ({})),
      api.opsChaosScenarios(token, true).catch(() => []),
      api.opsGlobalHealth().catch(() => ({ status: "error", service: "api" })),
      api.opsDbHealth().catch(() => ({ status: "error" })),
    ])
      .then(async ([circuitMap, activeChaos, globalHealth, dbHealth]) => {
        setCircuits(circuitMap);
        setChaosScenarios(activeChaos);
        setApiOnline(globalHealth.status === "ok");
        setDbOnline(dbHealth.status === "ok");

        const targetIds = deriveOperationalTargets(circuitMap);
        if (targetIds.length > 0) {
          const snapshots = await Promise.all(
            targetIds.map((tid) => api.opsHealthSnapshot(tid, token).catch(() => null))
          );
          const map: Record<string, HealthSnapshot | null> = {};
          targetIds.forEach((tid, i) => {
            map[tid] = snapshots[i];
          });
          setHealthMap(map);
        } else {
          setHealthMap({});
        }
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load operational snapshot.");
      })
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    loadOverview();
  }, [token]);

  const targetKeys = deriveOperationalTargets(circuits);
  const openCircuits = targetKeys.filter((k) => circuits[k]?.state === "OPEN");

  return (
    <>
      <PageHeader
        eyebrow="Operations Console"
        title="Network Overview"
        description="Authoritative status of participant targets, adaptive circuits, and resilience controllers."
        action={
          <Button variant="secondary" onClick={loadOverview}>
            <ConsoleIcon name="refresh" size={14} />
            Refresh State
          </Button>
        }
      />

      {error && <InlineError message={error} />}

      {loading ? (
        <ConsoleSkeleton />
      ) : (
        <>
          <section className="console-grid-4">
            <div className="console-card">
              <div className="console-card-header">
                <span className="console-card-kicker">Core System Readiness</span>
                <span className={`console-pill ${apiOnline && dbOnline ? "console-pill-success" : "console-pill-error"}`}>
                  {apiOnline && dbOnline ? "ONLINE" : "DEGRADED"}
                </span>
              </div>
              <div style={{ display: "flex", flexDirection: "column", gap: ".35rem", margin: ".35rem 0" }}>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", fontSize: ".82rem" }}>
                  <span style={{ color: "var(--muted)" }}>API Engine</span>
                  <span className={`console-pill ${apiOnline ? "console-pill-success" : "console-pill-error"}`}>
                    {apiOnline ? "READY" : "UNAVAILABLE"}
                  </span>
                </div>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", fontSize: ".82rem" }}>
                  <span style={{ color: "var(--muted)" }}>Database Ledger</span>
                  <span className={`console-pill ${dbOnline ? "console-pill-success" : "console-pill-error"}`}>
                    {dbOnline ? "READY" : "UNAVAILABLE"}
                  </span>
                </div>
              </div>
              <span className="console-meta-text">Authoritative: GET /health & /health/db</span>
            </div>

            <div className="console-card">
              <div className="console-card-header">
                <span className="console-card-kicker">Execution Targets</span>
                <span className={`console-pill ${openCircuits.length === 0 ? "console-pill-success" : "console-pill-warning"}`}>
                  {openCircuits.length === 0 ? "ALL ELIGIBLE" : `${openCircuits.length} INELIGIBLE`}
                </span>
              </div>
              <span className="console-val-large">{targetKeys.length} Configured</span>
              <span className="console-meta-text">
                {openCircuits.length === 0 ? "All circuit breakers closed" : `${openCircuits.join(", ")} circuit open`}
              </span>
            </div>

            <div className="console-card">
              <div className="console-card-header">
                <span className="console-card-kicker">Active Faults</span>
                <span className={`console-pill ${chaosScenarios.length > 0 ? "console-pill-warning" : "console-pill-neutral"}`}>
                  {chaosScenarios.length > 0 ? "CHAOS ENGAGED" : "NOMINAL"}
                </span>
              </div>
              <span className="console-val-large">{chaosScenarios.length}</span>
              <span className="console-meta-text">Injected faults currently active</span>
            </div>

            <div className="console-card">
              <div className="console-card-header">
                <span className="console-card-kicker">Reconciliation Engine</span>
                <span className="console-pill console-pill-neutral">PENDING M3-5</span>
              </div>
              <span className="console-val-large" style={{ fontSize: "1.1rem" }}>Not Available</span>
              <span className="console-meta-text">Operator orchestration pending M3-5</span>
            </div>
          </section>

          <section className="section-block">
            <div className="section-heading">
              <div>
                <p className="eyebrow">Circuit Breakers</p>
                <h2>Active Execution Targets</h2>
              </div>
              <button type="button" className="text-link" onClick={() => onNavigate("c-routing")}>
                View details & events →
              </button>
            </div>
            {targetKeys.length === 0 ? (
              <p className="console-meta-text">No execution targets configured on circuit breaker.</p>
            ) : (
              <div className="console-table-wrap">
                <table className="console-table">
                  <thead>
                    <tr>
                      <th>Target ID</th>
                      <th>Circuit State</th>
                      <th>Failures (Window)</th>
                      <th>Timeouts</th>
                      <th>Probes</th>
                      <th>Restoration</th>
                      <th>Last Evaluated</th>
                    </tr>
                  </thead>
                  <tbody>
                    {targetKeys.map((k) => {
                      const c = circuits[k];
                      return (
                        <tr key={k}>
                          <td className="mono" style={{ fontWeight: 600 }}>{c.executionTargetId}</td>
                          <td>
                            <span
                              className={`console-pill ${
                                c.state === "CLOSED" ? "console-pill-success" : c.state === "OPEN" ? "console-pill-error" : "console-pill-warning"
                              }`}
                            >
                              {c.state}
                            </span>
                          </td>
                          <td className="mono">{c.failureCount}</td>
                          <td className="mono">{c.timeoutCount}</td>
                          <td className="mono">{c.activeProbes} / {c.successfulProbes}</td>
                          <td className="mono">
                            {c.state === "CLOSED" ? "100%" : `${(c.restorationProgress * 100).toFixed(0)}% (Step ${c.restorationStep})`}
                          </td>
                          <td className="mono">{formatIso(c.lastEvaluatedAt)}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section className="section-block">
            <div className="section-heading">
              <div>
                <p className="eyebrow">Target Health</p>
                <h2>Operational Telemetry Summary</h2>
              </div>
              <button type="button" className="text-link" onClick={() => onNavigate("c-health")}>
                Manage health probes →
              </button>
            </div>
            {targetKeys.length === 0 ? (
              <div className="console-card">
                <p className="console-meta-text">No execution targets configured on circuit breaker.</p>
              </div>
            ) : (
              <div className="console-grid-2">
                {targetKeys.map((k) => (
                  <ParticipantHealthCard key={k} targetId={k} snapshot={healthMap[k] ?? null} />
                ))}
              </div>
            )}
          </section>

          <section className="section-block">
            <div className="section-heading">
              <div>
                <p className="eyebrow">Architecture Matrix</p>
                <h2>Subsystem Deployment State</h2>
              </div>
            </div>
            <div className="console-table-wrap">
              <table className="console-table">
                <thead>
                  <tr>
                    <th>Subsystem</th>
                    <th>Milestone</th>
                    <th>Authoritative Seam</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>Adaptive Routing Engine</td>
                    <td className="mono">M2</td>
                    <td className="mono">internal/payments/routing.go</td>
                    <td><span className="console-pill console-pill-success">OPERATIONAL</span></td>
                  </tr>
                  <tr>
                    <td>Target Circuit Breakers</td>
                    <td className="mono">M2</td>
                    <td className="mono">GET /api/ops/circuit</td>
                    <td><span className="console-pill console-pill-success">OPERATIONAL</span></td>
                  </tr>
                  <tr>
                    <td>Fault Injection Controller</td>
                    <td className="mono">M2</td>
                    <td className="mono">GET /api/ops/chaos/scenarios</td>
                    <td><span className="console-pill console-pill-success">OPERATIONAL</span></td>
                  </tr>
                  <tr>
                    <td>Canonical Hash Commitment</td>
                    <td className="mono">M3-1</td>
                    <td className="mono">internal/reconciliation/canonical.go</td>
                    <td><span className="console-pill console-pill-neutral">ENGINE ONLY</span></td>
                  </tr>
                  <tr>
                    <td>Merkle Leaf Buckets</td>
                    <td className="mono">M3-2</td>
                    <td className="mono">internal/reconciliation/merkle_bucket.go</td>
                    <td><span className="console-pill console-pill-neutral">ENGINE ONLY</span></td>
                  </tr>
                  <tr>
                    <td>Reconciliation Orchestration</td>
                    <td className="mono">M3-5</td>
                    <td className="mono">Pending M3-5 implementation</td>
                    <td><span className="console-pill console-pill-warning">NOT AVAILABLE</span></td>
                  </tr>
                  <tr>
                    <td>Ledger Invariant Engine</td>
                    <td className="mono">M3-7</td>
                    <td className="mono">Pending M3-7 implementation</td>
                    <td><span className="console-pill console-pill-warning">NOT AVAILABLE</span></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}
    </>
  );
}

function ParticipantHealthCard({ targetId, snapshot }: { targetId: string; snapshot: HealthSnapshot | null }) {
  if (!snapshot) {
    return (
      <div className="console-card">
        <div className="console-card-header">
          <span className="console-card-kicker">{targetId}</span>
          <span className="console-pill console-pill-neutral">NO SAMPLES</span>
        </div>
        <p className="console-meta-text">No health samples recorded in current rolling window.</p>
      </div>
    );
  }

  const scorePct = (snapshot.score * 100).toFixed(1);
  const availPct = (snapshot.availabilityScore * 100).toFixed(1);
  const succPct = (snapshot.successScore * 100).toFixed(1);

  return (
    <div className="console-card">
      <div className="console-card-header">
        <span className="console-card-kicker">{snapshot.targetId}</span>
        <span className={`console-pill ${snapshot.score >= 0.8 ? "console-pill-success" : snapshot.score >= 0.5 ? "console-pill-warning" : "console-pill-error"}`}>
          SCORE {scorePct}%
        </span>
      </div>
      <div className="console-grid-2" style={{ marginBottom: 0, gap: ".5rem" }}>
        <div>
          <span className="console-meta-text">Availability:</span>{" "}
          <strong className="mono" style={{ color: "var(--ink)" }}>{availPct}%</strong>
        </div>
        <div>
          <span className="console-meta-text">Success:</span>{" "}
          <strong className="mono" style={{ color: "var(--ink)" }}>{succPct}%</strong>
        </div>
      </div>
      <div className="console-grid-2" style={{ marginBottom: 0, gap: ".5rem" }}>
        <div>
          <span className="console-meta-text">Latency Penalty:</span>{" "}
          <span className="mono" style={{ color: "var(--muted)" }}>{snapshot.latencyPenalty.toFixed(3)}</span>
        </div>
        <div>
          <span className="console-meta-text">Timeout Penalty:</span>{" "}
          <span className="mono" style={{ color: "var(--muted)" }}>{snapshot.timeoutPenalty.toFixed(3)}</span>
        </div>
      </div>
      <span className="console-meta-text" style={{ borderTop: "1px solid var(--line)", paddingTop: ".5rem", marginTop: ".25rem" }}>
        Samples: <strong className="mono">{snapshot.sampleCount}</strong> · Computed: <span className="mono">{formatIso(snapshot.computedAt)}</span>
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// 2. Bank Health View
// ---------------------------------------------------------------------------

function ConsoleHealthView({ token }: { token: string }) {
  const [targetId, setTargetId] = useState("");
  const [targetKeys, setTargetKeys] = useState<string[]>([]);
  const [snapshot, setSnapshot] = useState<HealthSnapshot | null>(null);
  const [probing, setProbing] = useState(false);
  const [lastProbe, setLastProbe] = useState<HealthSample | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  function loadTargetsAndSnapshot(currentTarget?: string) {
    setLoading(true);
    setError("");
    api.opsCircuits(token)
      .then((circuitMap) => {
        const derived = deriveOperationalTargets(circuitMap);
        setTargetKeys(derived);
        const nextTarget = currentTarget && derived.includes(currentTarget)
          ? currentTarget
          : derived[0] ?? "";
        setTargetId(nextTarget);
        if (nextTarget) {
          return api.opsHealthSnapshot(nextTarget, token).then(setSnapshot);
        } else {
          setSnapshot(null);
        }
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Health data unavailable.");
        setSnapshot(null);
      })
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    loadTargetsAndSnapshot();
  }, [token]);

  function switchTarget(t: string) {
    setTargetId(t);
    setLoading(true);
    setError("");
    api.opsHealthSnapshot(t, token)
      .then(setSnapshot)
      .catch((err) => {
        setError(err instanceof Error ? err.message : `Health data unavailable for ${t}.`);
        setSnapshot(null);
      })
      .finally(() => setLoading(false));
  }

  function handleProbe() {
    if (!targetId) return;
    setProbing(true);
    setError("");
    api.opsHealthSample(targetId, token)
      .then((sample) => {
        setLastProbe(sample);
        return api.opsHealthSnapshot(targetId, token).then(setSnapshot);
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to record health probe.");
      })
      .finally(() => setProbing(false));
  }

  return (
    <>
      <PageHeader
        eyebrow="Target Telemetry"
        title="Bank Health Diagnostics"
        description="Real-time participant availability and latency scoring evaluated over rolling observation windows."
        action={
          <Button onClick={handleProbe} disabled={probing || !targetId}>
            <ConsoleIcon name="pulse" size={15} />
            {probing ? "Probing Target…" : targetId ? `Trigger ${targetId} Probe` : "Probe Unavailable"}
          </Button>
        }
      />

      {targetKeys.length === 0 ? (
        <div className="console-card" style={{ marginBottom: "1.25rem" }}>
          <span className="console-card-kicker">Target Telemetry</span>
          <p className="console-meta-text">No operational targets registered. Health probes require registered execution targets.</p>
        </div>
      ) : (
        <div className="console-tabs">
          {targetKeys.map((k) => (
            <button
              key={k}
              type="button"
              className={`console-tab ${targetId === k ? "is-active" : ""}`}
              onClick={() => switchTarget(k)}
            >
              Target: {k}
            </button>
          ))}
        </div>
      )}

      {error && <InlineError message={error} />}

      {lastProbe && (
        <div className="console-card" style={{ borderColor: "var(--accent)" }}>
          <div className="console-card-header">
            <span className="console-card-kicker">Latest Health Sample Probe</span>
            <span className={`console-pill ${lastProbe.Outcome === "SUCCESS" ? "console-pill-success" : "console-pill-error"}`}>
              {lastProbe.Outcome}
            </span>
          </div>
          <div className="console-grid-4" style={{ marginBottom: 0 }}>
            <div>
              <span className="console-meta-text">Target:</span>{" "}
              <strong className="mono">{lastProbe.TargetID}</strong>
            </div>
            <div>
              <span className="console-meta-text">Available:</span>{" "}
              <strong className="mono">{lastProbe.Available ? "YES" : "NO"}</strong>
            </div>
            <div>
              <span className="console-meta-text">Latency:</span>{" "}
              <strong className="mono">{(lastProbe.Latency / 1_000_000).toFixed(2)} ms</strong>
            </div>
            <div>
              <span className="console-meta-text">Sampled:</span>{" "}
              <span className="mono">{formatIso(lastProbe.SampledAt)}</span>
            </div>
          </div>
        </div>
      )}

      {loading ? (
        <ConsoleSkeleton />
      ) : !snapshot ? (
        <div className="console-card">
          <div className="console-card-header">
            <span className="console-card-kicker">{targetId}</span>
            <span className="console-pill console-pill-neutral">NO DATA</span>
          </div>
          <p className="console-meta-text">No health samples recorded yet for {targetId}. Use the probe button above to record a sample.</p>
        </div>
      ) : (
        <>
          <section className="console-grid-4">
            <div className="console-card">
              <span className="console-card-kicker">Composite Health Score</span>
              <span className="console-val-large" style={{ color: snapshot.score >= 0.8 ? "var(--success)" : snapshot.score >= 0.5 ? "var(--warning)" : "var(--error)" }}>
                {(snapshot.score * 100).toFixed(1)}%
              </span>
              <span className="console-meta-text">Weighted operational score</span>
            </div>

            <div className="console-card">
              <span className="console-card-kicker">Availability Score</span>
              <span className="console-val-large">{(snapshot.availabilityScore * 100).toFixed(1)}%</span>
              <span className="console-meta-text">35% formula weight</span>
            </div>

            <div className="console-card">
              <span className="console-card-kicker">Success Score</span>
              <span className="console-val-large">{(snapshot.successScore * 100).toFixed(1)}%</span>
              <span className="console-meta-text">35% formula weight</span>
            </div>

            <div className="console-card">
              <span className="console-card-kicker">Window Sample Count</span>
              <span className="console-val-large">{snapshot.sampleCount}</span>
              <span className="console-meta-text">Samples in 15m window</span>
            </div>
          </section>

          <section className="console-card">
            <span className="console-card-kicker">Telemetry Window Metadata</span>
            <div className="console-spec-list">
              <div className="console-spec-item">
                <span className="console-spec-label">Target Identifier</span>
                <span className="console-spec-val">{snapshot.targetId}</span>
              </div>
              <div className="console-spec-item">
                <span className="console-spec-label">Latency Penalty Applied</span>
                <span className="console-spec-val">{snapshot.latencyPenalty.toFixed(4)}</span>
              </div>
              <div className="console-spec-item">
                <span className="console-spec-label">Timeout Penalty Applied</span>
                <span className="console-spec-val">{snapshot.timeoutPenalty.toFixed(4)}</span>
              </div>
              <div className="console-spec-item">
                <span className="console-spec-label">Observation Window Started</span>
                <span className="console-spec-val">{formatIso(snapshot.windowStartedAt)}</span>
              </div>
              <div className="console-spec-item">
                <span className="console-spec-label">Computed At</span>
                <span className="console-spec-val">{formatIso(snapshot.computedAt)}</span>
              </div>
            </div>
          </section>

          <p className="console-meta-text" style={{ marginTop: "1rem" }}>
            Note: Displaying authoritative rolling observation window state. Historical time-series telemetry storage is not maintained by the backend.
          </p>
        </>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// 3. Routing & Circuits View
// ---------------------------------------------------------------------------

function ConsoleRoutingView({ token }: { token: string }) {
  const [circuits, setCircuits] = useState<Record<string, CircuitTargetSnapshot>>({});
  const [selectedTarget, setSelectedTarget] = useState<string>("BANK-A");
  const [events, setEvents] = useState<CircuitTransitionEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [error, setError] = useState("");

  function loadCircuits() {
    setLoading(true);
    setError("");
    api.opsCircuits(token)
      .then((map) => {
        setCircuits(map);
        const keys = Object.keys(map);
        if (keys.length > 0 && !keys.includes(selectedTarget)) {
          setSelectedTarget(keys[0]);
        }
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load circuit states.");
      })
      .finally(() => setLoading(false));
  }

  function loadEvents(target: string) {
    setEventsLoading(true);
    api.opsCircuitEvents(target, token)
      .then(setEvents)
      .catch(() => setEvents([]))
      .finally(() => setEventsLoading(false));
  }

  useEffect(() => {
    loadCircuits();
  }, [token]);

  useEffect(() => {
    if (selectedTarget) {
      loadEvents(selectedTarget);
    }
  }, [selectedTarget, token]);

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
                    currentCircuit.state === "CLOSED" ? "console-pill-success" : currentCircuit.state === "OPEN" ? "console-pill-error" : "console-pill-warning"
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
                <span className="console-meta-text">Step {currentCircuit.restorationStep} of {currentCircuit.maxRestorationSteps || 1}</span>
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
            {eventsLoading ? (
              <ConsoleSkeleton />
            ) : events.length === 0 ? (
              <div className="console-card">
                <p className="console-meta-text">No state transition events logged for {selectedTarget} yet.</p>
              </div>
            ) : (
              <div className="console-table-wrap">
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
                              ev.newState === "CLOSED" ? "console-pill-success" : ev.newState === "OPEN" ? "console-pill-error" : "console-pill-warning"
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

// ---------------------------------------------------------------------------
// 4. Reconciliation View (M3-5 Placeholder)
// ---------------------------------------------------------------------------

function ConsoleReconciliationView() {
  return (
    <>
      <PageHeader
        eyebrow="Reconciliation"
        title="Reconciliation Orchestration"
        description="Automated multi-participant ledger comparison, discrepancy identification, and settlement convergence."
      />
      <div className="console-notice-box">
        <span className="console-notice-tag">MODULE NOT AVAILABLE YET · MILESTONE M3-5</span>
        <h3>Backend reconciliation orchestration is not available yet.</h3>
        <p>
          The underlying data foundation (canonical transaction commitments, deterministic Merkle buckets, and incremental tree models) has been verified in internal packages, but operator-facing reconciliation run orchestration, discrepancy APIs, and automatic settlement batches are scheduled for future milestone integration.
        </p>
        <div className="console-spec-list">
          <div className="console-spec-item">
            <span className="console-spec-label">Canonical Commitment Foundation</span>
            <span className="console-spec-val">Implemented (internal/reconciliation/canonical.go)</span>
          </div>
          <div className="console-spec-item">
            <span className="console-spec-label">Merkle Bucket Partitioning</span>
            <span className="console-spec-val">Implemented (internal/reconciliation/merkle_bucket.go)</span>
          </div>
          <div className="console-spec-item">
            <span className="console-spec-label">Reconciliation Execution Seam</span>
            <span className="console-spec-val">Pending M3-5 Backend Integration</span>
          </div>
          <div className="console-spec-item">
            <span className="console-spec-label">Recorded Run History</span>
            <span className="console-spec-val">0 runs (no fabricated records)</span>
          </div>
        </div>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// 5. Merkle View (M3-2 / M3-3 Explorer Placeholder)
// ---------------------------------------------------------------------------

function ConsoleMerkleView() {
  return (
    <>
      <PageHeader
        eyebrow="Cryptographic Proofs"
        title="Merkle Tree Verification"
        description="Root hash commitment structures, bucket partition trees, and cryptographic membership inclusion verification."
      />
      <div className="console-notice-box">
        <span className="console-notice-tag">MODULE NOT AVAILABLE YET · MILESTONE M3-8</span>
        <h3>Operator Merkle visualization API is not available yet.</h3>
        <p>
          Incremental binary Merkle trees and bucket digest generation are implemented and tested within the core ledger engine. Live operator tree traversal endpoints have not yet been exposed by the backend API. In accordance with zero-fabrication safety rules, tree visualizations are not synthesized on the client.
        </p>
        <div className="console-spec-list">
          <div className="console-spec-item">
            <span className="console-spec-label">Tree Topology</span>
            <span className="console-spec-val">Binary incremental Merkle tree with prefix-bucket leaves</span>
          </div>
          <div className="console-spec-item">
            <span className="console-spec-label">Hash Algorithm</span>
            <span className="console-spec-val">SHA-256 (64 hex characters)</span>
          </div>
          <div className="console-spec-item">
            <span className="console-spec-label">Operator Visualizer Seam</span>
            <span className="console-spec-val">Pending M3-8 Backend Integration</span>
          </div>
        </div>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// 6. Integrity View (M3-7 Placeholder)
// ---------------------------------------------------------------------------

function ConsoleIntegrityView() {
  return (
    <>
      <PageHeader
        eyebrow="Invariant Verification"
        title="Ledger Integrity Engine"
        description="Continuous verification of financial invariants across double-entry participant books."
      />
      <div className="console-notice-box">
        <span className="console-notice-tag">MODULE NOT AVAILABLE YET · MILESTONE M3-7</span>
        <h3>Automated integrity invariant scans are not available yet.</h3>
        <p>
          Backend verification routines for money conservation, double-entry completeness, non-negative balance checks, and idempotent uniqueness are scheduled for integration in Milestone M3-7. Live invariant pass/fail metrics will be rendered when authoritative endpoints are active.
        </p>
        <div className="console-spec-list">
          <div className="console-spec-item">
            <span className="console-spec-label">Conservation of Money Invariant</span>
            <span className="console-spec-val">∑ Debits == ∑ Credits (Pending M3-7)</span>
          </div>
          <div className="console-spec-item">
            <span className="console-spec-label">Account Non-Negative Balance Invariant</span>
            <span className="console-spec-val">Enforced at DB seam (Console scan pending M3-7)</span>
          </div>
          <div className="console-spec-item">
            <span className="console-spec-label">Transaction Uniqueness Invariant</span>
            <span className="console-spec-val">Deterministic clientRequestId (Pending M3-7)</span>
          </div>
        </div>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// 7. Chaos View (Real M2 Chaos Controller)
// ---------------------------------------------------------------------------

function ConsoleChaosView({ token }: { token: string }) {
  const [scenarios, setScenarios] = useState<ChaosScenario[]>([]);
  const [circuitTargets, setCircuitTargets] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [feedback, setFeedback] = useState("");

  // Form states for creating a real scenario
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
  }, [token]);

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
        loadData();
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to reset chaos."));
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

      {circuitTargets.length === 0 && !loading && (
        <div className="console-notice-box" style={{ marginBottom: "1.25rem", padding: "1rem" }}>
          <span className="console-notice-tag">TARGET REGISTRATION REQUIRED</span>
          <p style={{ margin: 0 }}>
            No execution targets are registered with the circuit breaker (<code>GET /api/ops/circuit</code> returned 0 targets). Scenario submission is disabled until operational targets are available.
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
            min={100}
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
                <p className="eyebrow">Active Scenarios</p>
                <h2>Currently Running Injections ({activeList.length})</h2>
              </div>
            </div>
            {activeList.length === 0 ? (
              <div className="console-card">
                <p className="console-meta-text">No active chaos scenarios. The system is operating in nominal conditions.</p>
              </div>
            ) : (
              <div className="console-table-wrap">
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
              <div className="console-table-wrap">
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

// ---------------------------------------------------------------------------
// 8. Activity View
// ---------------------------------------------------------------------------

function ConsoleActivityView({ token }: { token: string }) {
  const [events, setEvents] = useState<CircuitTransitionEvent[]>([]);
  const [configuredTargets, setConfiguredTargets] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  function loadEvents() {
    setLoading(true);
    setError("");
    api.opsCircuits(token)
      .then((circuitMap) => {
        const targetIds = deriveOperationalTargets(circuitMap);
        setConfiguredTargets(targetIds);
        if (targetIds.length === 0) {
          setEvents([]);
          return;
        }
        return Promise.all(
          targetIds.map((tid) => api.opsCircuitEvents(tid, token).catch(() => []))
        ).then((results) => {
          const combined = results.flat();
          combined.sort(
            (a, b) => new Date(b.transitionedAt).getTime() - new Date(a.transitionedAt).getTime()
          );
          setEvents(combined);
        });
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load circuit events.");
      })
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    loadEvents();
  }, [token]);

  return (
    <>
      <PageHeader
        eyebrow="Audit Feed"
        title="Operational Activity Log"
        description="Chronological log of adaptive circuit state transitions, trips, and restoration events across operational targets."
        action={
          <Button variant="secondary" onClick={loadEvents}>
            <ConsoleIcon name="refresh" size={14} />
            Refresh
          </Button>
        }
      />

      {error && <InlineError message={error} />}

      {loading ? (
        <ConsoleSkeleton />
      ) : configuredTargets.length === 0 ? (
        <div className="console-card">
          <div className="console-card-header">
            <span className="console-card-kicker">Activity Log</span>
            <span className="console-pill console-pill-warning">NO TARGETS CONFIGURED</span>
          </div>
          <p className="console-meta-text">
            No execution targets were returned by <code>GET /api/ops/circuit</code>. Circuit transition events cannot be streamed until operational targets are registered.
          </p>
        </div>
      ) : events.length === 0 ? (
        <div className="console-card">
          <span className="console-card-kicker">Activity Log</span>
          <p className="console-meta-text">No circuit state transitions or recovery events recorded yet for configured targets ({configuredTargets.join(", ")}).</p>
        </div>
      ) : (
        <section className="section-block">
          <div className="console-table-wrap">
            <table className="console-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Target</th>
                  <th>Event</th>
                  <th>Reason</th>
                  <th>Failures</th>
                  <th>Timeouts</th>
                  <th>Restoration Step</th>
                </tr>
              </thead>
              <tbody>
                {events.map((ev, i) => (
                  <tr key={ev.id ?? i}>
                    <td className="mono">{formatIso(ev.transitionedAt)}</td>
                    <td className="mono" style={{ fontWeight: 600 }}>{ev.executionTargetId}</td>
                    <td>
                      <span className="console-pill console-pill-neutral" style={{ marginRight: ".3rem" }}>
                        {ev.previousState}
                      </span>
                      →
                      <span
                        className={`console-pill ${
                          ev.newState === "CLOSED" ? "console-pill-success" : ev.newState === "OPEN" ? "console-pill-error" : "console-pill-warning"
                        }`}
                        style={{ marginLeft: ".3rem" }}
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
        </section>
      )}

      <p className="console-meta-text" style={{ marginTop: "1.5rem" }}>
        Note: The operational log aggregates real circuit breaker transition records. Aggregated cross-bank ledger transaction stream is scheduled for future milestone integration (M3-8).
      </p>
    </>
  );
}

// ---------------------------------------------------------------------------
// Helpers & Icons
// ---------------------------------------------------------------------------

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

function formatIso(isoString?: string) {
  if (!isoString) return "—";
  try {
    const d = new Date(isoString);
    return new Intl.DateTimeFormat("en-IN", {
      month: "short",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    }).format(d);
  } catch {
    return isoString;
  }
}

type ConsoleIconName =
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

function ConsoleIcon({ name, size = 18 }: { name: ConsoleIconName; size?: number }) {
  const paths: Record<ConsoleIconName, React.ReactNode> = {
    overview: (
      <>
        <rect x="2.5" y="2.5" width="5" height="5" rx="1" />
        <rect x="10.5" y="2.5" width="5" height="5" rx="1" />
        <rect x="2.5" y="10.5" width="5" height="5" rx="1" />
        <rect x="10.5" y="10.5" width="5" height="5" rx="1" />
      </>
    ),
    health: <path d="M2.5 9h3l2-5 2 10 2-5h4" />,
    routing: (
      <>
        <path d="M4 5h5a4 4 0 0 1 4 4v5" />
        <path d="M4 13h5a4 4 0 0 0 4-4V5" />
        <circle cx="4" cy="5" r="1.5" />
        <circle cx="4" cy="13" r="1.5" />
      </>
    ),
    reconciliation: (
      <>
        <rect x="3" y="3.5" width="12" height="4" rx="1" />
        <rect x="3" y="10.5" width="12" height="4" rx="1" />
      </>
    ),
    merkle: (
      <>
        <circle cx="9" cy="4" r="1.5" />
        <circle cx="5" cy="13" r="1.5" />
        <circle cx="13" cy="13" r="1.5" />
        <path d="m9 5.5-4 6M9 5.5l4 6" />
      </>
    ),
    integrity: <path d="M9 2.5 3.5 5v4c0 3.8 5.5 5.5 5.5 5.5s5.5-1.7 5.5-5.5V5L9 2.5Z" />,
    chaos: <path d="m9.5 2-6 8h5l-1 6 6-8h-5l1-6Z" />,
    activity: (
      <>
        <path d="M3 5h12M3 9h12M3 13h12" />
      </>
    ),
    pulse: <path d="M2.5 9h3l2-5 2 10 2-5h4" />,
    refresh: (
      <>
        <path d="M14 7.5A5.5 5.5 0 1 0 14.5 11" />
        <path d="M14.5 5v3h-3" />
      </>
    ),
  };

  return (
    <svg
      aria-hidden="true"
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 18 18"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {paths[name]}
    </svg>
  );
}
