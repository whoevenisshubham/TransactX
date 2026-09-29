import { useEffect, useState } from "react";
import { api, ApiError } from "./api";
import { Avatar, BrandMark, Button, InlineError, PageHeader, Skeleton } from "./components";
import { subscribeOperationalEvents, type StreamStatus } from "./operationalEvents";
import type {
  ActivityEvent,
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
  const [liveRevision, setLiveRevision] = useState(0);
  const [streamStatus, setStreamStatus] = useState<StreamStatus>("connecting");

  useEffect(() => {
    const handle = () => setView(readConsoleView());
    window.addEventListener("popstate", handle);
    return () => window.removeEventListener("popstate", handle);
  }, []);

  useEffect(() => subscribeOperationalEvents(
    token,
    () => setLiveRevision((revision) => revision + 1),
    setStreamStatus,
  ), [token]);

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
          <div className="console-card-header" style={{ justifyContent: "flex-end", marginBottom: ".6rem" }}>
            <span className={`console-pill ${streamStatus === "connected" ? "console-pill-success" : streamStatus === "connecting" ? "console-pill-warning" : "console-pill-error"}`}>
              LIVE HINTS {streamStatus.toUpperCase()}
            </span>
          </div>
          {view === "c-overview" && <ConsoleOverviewView token={token} onNavigate={navigate} liveRevision={liveRevision} />}
          {view === "c-health" && <ConsoleHealthView token={token} liveRevision={liveRevision} />}
          {view === "c-routing" && <ConsoleRoutingView token={token} liveRevision={liveRevision} />}
          {view === "c-reconciliation" && <ConsoleReconciliationView token={token} liveRevision={liveRevision} />}
          {view === "c-merkle" && <ConsoleMerkleView token={token} />}
          {view === "c-integrity" && <ConsoleIntegrityView token={token} liveRevision={liveRevision} />}
          {view === "c-chaos" && <ConsoleChaosView token={token} liveRevision={liveRevision} />}
          {view === "c-activity" && <ConsoleActivityView token={token} liveRevision={liveRevision} />}
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

function ConsoleOverviewView({ token, onNavigate, liveRevision }: { token: string; onNavigate: (v: ConsoleView) => void; liveRevision: number }) {
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
  }, [token, liveRevision]);

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

function ConsoleHealthView({ token, liveRevision }: { token: string; liveRevision: number }) {
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
    loadTargetsAndSnapshot(targetId || undefined);
  }, [token, liveRevision]);

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

function ConsoleRoutingView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [distribution, setDistribution] = useState<{ targetId: string; payments: number }[]>([]);
  const [distributionError, setDistributionError] = useState("");
  const [circuits, setCircuits] = useState<Record<string, CircuitTargetSnapshot>>({});
  const [selectedTarget, setSelectedTarget] = useState<string>("BANK-A");
  const [events, setEvents] = useState<CircuitTransitionEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [error, setError] = useState("");

  function loadDistribution() {
    api.opsRoutingDistribution(token)
      .then(data => { setDistribution(data.items); setDistributionError(""); })
      .catch(err => setDistributionError(err instanceof Error ? err.message : "Could not load route selections."));
  }
  useEffect(() => { loadDistribution(); }, [token, liveRevision]);

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
        {distribution.map(item => <div className="console-spec-item" key={item.targetId}>
          <span>{item.targetId}</span>
          <span style={{ minWidth: "40%" }}>
            <span style={{ display: "inline-block", height: ".6rem", background: "var(--accent)", width: `${Math.round(item.payments / Math.max(1, ...distribution.map(x => x.payments)) * 100)}%`, marginRight: ".5rem" }} />
            {item.payments} payments
          </span>
        </div>)}
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
// 4. Reconciliation View
// ---------------------------------------------------------------------------

function ConsoleReconciliationView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [runs, setRuns] = useState<any[]>([]);
  const [selected, setSelected] = useState<any>(null);
  const [discrepancies, setDiscrepancies] = useState<any[]>([]);
  const [participantId, setParticipantId] = useState("");
  const [scopeFrom, setScopeFrom] = useState("");
  const [scopeTo, setScopeTo] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function refresh() {
    try { const page = await api.opsReconciliationRuns(token); setRuns(page.items ?? []); setError(""); }
    catch (err) { setError(err instanceof Error ? err.message : "Could not load reconciliation history."); }
  }
  useEffect(() => { void refresh(); }, [token, liveRevision]);
  async function choose(run: any) {
    setSelected(run);
    try { const page = await api.opsReconciliationDiscrepancies(run.id, token); setDiscrepancies(page.items ?? []); setError(""); }
    catch (err) { setError(err instanceof Error ? err.message : "Could not load discrepancy evidence."); }
  }
  async function execute(event: React.FormEvent) {
    event.preventDefault(); setBusy(true); setError("");
    try {
      const run = await api.opsReconciliationCreateRun({ participantId, scopeFrom, scopeTo }, token);
      await refresh(); await choose(run);
    } catch (err) { setError(err instanceof Error ? err.message : "Reconciliation failed."); }
    finally { setBusy(false); }
  }
  return (
    <>
      <PageHeader
        eyebrow="Reconciliation"
        title="Reconciliation Runs"
        description="Compare central and participant ledger commitments over the same UTC scope."
      />
      <div className="console-card">
        <h3>Run reconciliation</h3>
        <form className="admin-form" onSubmit={execute}>
          <div className="form-group"><label>Participant ID</label><input value={participantId} onChange={e => setParticipantId(e.target.value)} required /></div>
          <div className="form-group"><label>Scope From (ISO8601)</label><input value={scopeFrom} onChange={e => setScopeFrom(e.target.value)} required /></div>
          <div className="form-group"><label>Scope To (ISO8601)</label><input value={scopeTo} onChange={e => setScopeTo(e.target.value)} required /></div>
          <Button type="submit" disabled={busy}>{busy ? "Running..." : "Run reconciliation"}</Button>
        </form>
      </div>
      {error && <InlineError message={error} />}
      <div className="console-card">
        <h3>Recorded runs</h3>
        <Button variant="secondary" onClick={refresh}>Refresh</Button>
        {runs.length === 0 && <p>No reconciliation runs recorded.</p>}
        {runs.length > 0 && <div className="console-table-wrap"><table className="console-table">
          <thead><tr><th>Started</th><th>Participant</th><th>Status</th><th>Discrepancies</th><th>Detail</th></tr></thead>
          <tbody>{runs.map(run => <tr key={run.id}>
            <td>{run.startedAt}</td><td>{run.participantId}</td><td>{run.status}</td><td>{run.discrepancyCount}</td>
            <td><button type="button" className="text-link" onClick={() => choose(run)}>View</button></td>
          </tr>)}</tbody>
        </table></div>}
      </div>
      {selected && <div className="console-card">
        <h3>Run {selected.id}</h3>
        <p>Scope: {selected.scopeFrom} to {selected.scopeTo}</p>
        <p>Records: {selected.recordCount} · Status: {selected.status} · Discrepancies: {selected.discrepancyCount}</p>
        <div className="console-spec-grid">
          <div className="console-spec-item"><span>Elapsed</span><strong>{(selected.elapsedNs / 1_000_000).toFixed(2)} ms</strong></div>
          <div className="console-spec-item"><span>Merkle nodes visited</span><strong>{selected.nodesVisited}</strong></div>
          <div className="console-spec-item"><span>Records inspected</span><strong>{selected.recordsInspected}</strong></div>
          <div className="console-spec-item"><span>Bytes examined</span><strong>{selected.bytesExamined}</strong></div>
          <div className="console-spec-item"><span>Divergent buckets</span><strong>{selected.divergentBuckets}</strong></div>
          <div className="console-spec-item"><span>Divergent records</span><strong>{selected.divergentRecords}</strong></div>
        </div>
        {selected.errorMessage && <InlineError message={selected.errorMessage} />}
        {discrepancies.length === 0 && <p>No discrepancy evidence in this page.</p>}
        {discrepancies.map((item, index) => <div className="console-spec-item" key={item.id ?? index}>
          <span>{item.mismatchCategory} · {item.bucketKey}</span><span>{item.evidence?.operation_id ?? "bucket-level"}</span>
        </div>)}
      </div>
      }
    </>
  );
}

// ---------------------------------------------------------------------------
// 5. Merkle View (M3-2 / M3-3 Explorer Placeholder)
// ---------------------------------------------------------------------------

function ConsoleMerkleView({ token }: { token: string }) {
  const [participantId, setParticipantId] = useState("");
  const [scopeFrom, setScopeFrom] = useState("");
  const [scopeTo, setScopeTo] = useState("");

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [root, setRoot] = useState<import("./types").MerkleTreeRoot | null>(null);
  const [childrenMap, setChildrenMap] = useState<Record<string, import("./types").MerkleTreeChild[]>>({});

  async function fetchRoot(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    setRoot(null);
    setChildrenMap({});
    try {
      const res = await api.opsReconciliationTreeRoot(participantId, scopeFrom, scopeTo, token);
      setRoot(res);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Failed to fetch Merkle root");
    } finally {
      setLoading(false);
    }
  }

  async function fetchChildren(generation: string, path: string) {
    const key = `${generation}:${path}`;
    if (childrenMap[key]) return; // already fetched
    try {
      const res = await api.opsReconciliationTreeChildren(participantId, scopeFrom, scopeTo, generation, path, token);
      setChildrenMap(prev => ({ ...prev, [key]: res }));
    } catch (err) {
      console.error("Failed to fetch children", err);
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Cryptographic Proofs"
        title="Merkle Tree Verification"
        description="Root hash commitment structures, bucket partition trees, and cryptographic membership inclusion verification."
      />
      <div className="console-card">
        <h3>Inspect Tree Root</h3>
        <form className="admin-form" onSubmit={fetchRoot}>
          <div className="form-group">
            <label>Participant ID</label>
            <input value={participantId} onChange={(e) => setParticipantId(e.target.value)} required />
          </div>
          <div className="form-group">
            <label>Scope From (ISO8601)</label>
            <input value={scopeFrom} onChange={(e) => setScopeFrom(e.target.value)} required />
          </div>
          <div className="form-group">
            <label>Scope To (ISO8601)</label>
            <input value={scopeTo} onChange={(e) => setScopeTo(e.target.value)} required />
          </div>
          <div className="form-actions">
            <Button type="submit" disabled={loading}>Fetch Root</Button>
          </div>
        </form>
        {error && <InlineError message={error} />}
      </div>

      {root && (
        <div className="console-card">
          <h3>Merkle Root</h3>
          <div className="console-spec-list">
            <div className="console-spec-item">
              <span className="console-spec-label">Root Hash</span>
              <span className="console-spec-val" style={{fontFamily: "monospace", wordBreak: "break-all"}}>{root.rootHex}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Algorithm</span>
              <span className="console-spec-val">{root.algorithm}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Version</span>
              <span className="console-spec-val">{root.version}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Generation</span>
              <span className="console-spec-val">{root.ref.Generation}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Logical Region</span>
              <span className="console-spec-val">{root.region.Start} - {root.region.End}</span>
            </div>
          </div>
          <div style={{marginTop: "1.5rem"}}>
            <Button variant="secondary" onClick={() => fetchChildren(root.ref.Generation, root.ref.Path)}>
              Load Children
            </Button>
          </div>

          {childrenMap[`${root.ref.Generation}:${root.ref.Path}`] && (
            <div style={{marginTop: "1rem"}}>
              <h4>Children:</h4>
              {childrenMap[`${root.ref.Generation}:${root.ref.Path}`].map((c, i) => (
                <div key={i} className="console-spec-list" style={{background: "var(--bg-subtle)", padding: "0.5rem", marginBottom: "0.5rem", borderRadius: "4px"}}>
                  <div className="console-spec-item">
                    <span className="console-spec-label">Hash</span>
                    <span className="console-spec-val" style={{fontFamily: "monospace", fontSize: "0.85rem"}}>{c.hashHex}</span>
                  </div>
                  <div className="console-spec-item">
                    <span className="console-spec-label">Path</span>
                    <span className="console-spec-val">{c.ref.Path}</span>
                  </div>
                  <Button variant="quiet" onClick={() => fetchChildren(c.ref.Generation, c.ref.Path)} style={{marginTop: "0.5rem"}}>
                    Expand
                  </Button>
                  {childrenMap[`${c.ref.Generation}:${c.ref.Path}`] && (
                    <div style={{paddingLeft: "1rem", marginTop: "0.5rem", borderLeft: "2px solid var(--border-color)"}}>
                      {childrenMap[`${c.ref.Generation}:${c.ref.Path}`].map((cc, ci) => (
                        <div key={ci} className="console-spec-item" style={{display: "block", marginBottom: "0.25rem"}}>
                          <span className="console-spec-label">Child {cc.ref.Path}:</span>
                          <span className="console-spec-val" style={{fontFamily: "monospace", fontSize: "0.85rem", display: "block"}}>{cc.hashHex}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// 6. Integrity View
// ---------------------------------------------------------------------------

function ConsoleIntegrityView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [integrityRuns, setIntegrityRuns] = useState<any[]>([]);
  const [integrityDetail, setIntegrityDetail] = useState<any>(null);
  const [integrityError, setIntegrityError] = useState("");
  const [checking, setChecking] = useState(false);
  const [operationId, setOperationId] = useState("");
  const [participantId, setParticipantId] = useState("");
  const [scopeFrom, setScopeFrom] = useState("");
  const [scopeTo, setScopeTo] = useState("");

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [proof, setProof] = useState<import("./types").IntegrityProof | null>(null);

  const [verifying, setVerifying] = useState(false);
  const [verifyResult, setVerifyResult] = useState<any>(null);
  const [verifyError, setVerifyError] = useState("");

  async function loadIntegrity() {
    try {
      const status = await api.opsIntegrityStatus(token);
      setIntegrityRuns(status.runs ?? []);
      if (status.runs?.[0]) setIntegrityDetail(await api.opsIntegrityRun(status.runs[0].runId, token));
      setIntegrityError("");
    } catch (err) { setIntegrityError(err instanceof Error ? err.message : "Could not load integrity state."); }
  }
  useEffect(() => { void loadIntegrity(); }, [token, liveRevision]);
  async function runIntegrity() {
    setChecking(true); setIntegrityError("");
    try {
      const input: Record<string, unknown> = {};
      if (participantId.trim() && scopeFrom && scopeTo) {
        input.participantId = participantId.trim(); input.scope = { from: scopeFrom, to: scopeTo };
      }
      const result = await api.opsIntegrityCheck(input, token);
      setIntegrityDetail(result); await loadIntegrity();
    } catch (err) { setIntegrityError(err instanceof Error ? err.message : "Integrity check failed."); }
    finally { setChecking(false); }
  }

  async function fetchProof(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    setProof(null);
    setVerifyResult(null);
    setVerifyError("");
    try {
      const res = await api.opsReconciliationGetProof(operationId, participantId, scopeFrom, scopeTo, token);
      setProof(res);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Failed to fetch integrity proof");
    } finally {
      setLoading(false);
    }
  }

  async function handleVerify() {
    if (!proof) return;
    setVerifying(true);
    setVerifyError("");
    setVerifyResult(null);
    try {
      const res = await api.opsReconciliationVerifyProof(participantId, scopeFrom, scopeTo, proof, token);
      setVerifyResult(res);
    } catch (err) {
      if (err instanceof ApiError) setVerifyError(err.message);
      else setVerifyError("Failed to verify proof");
    } finally {
      setVerifying(false);
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Invariant Verification"
        title="Ledger Integrity Engine"
        description="Run and review persisted financial invariant checks."
      />
      <div className="console-card">
        <h3>Financial integrity</h3>
        <p>Run the general checks, or fill participant and scope below to include Merkle commitment consistency.</p>
        <Button onClick={runIntegrity} disabled={checking}>{checking ? "Checking..." : "Run integrity checks"}</Button>
        <Button variant="secondary" onClick={loadIntegrity}>Refresh history</Button>
        {integrityError && <InlineError message={integrityError} />}
        {integrityRuns.length === 0 && <p>No integrity checks recorded.</p>}
        {integrityRuns.map(run => <button type="button" className="text-link" key={run.runId} onClick={async () => {
          try { setIntegrityDetail(await api.opsIntegrityRun(run.runId, token)); setIntegrityError(""); }
          catch (err) { setIntegrityError(err instanceof Error ? err.message : "Could not load integrity run."); }
        }} style={{ display: "block", marginTop: "0.8rem" }}>
          {run.startedAt} · {run.status} · {run.summary.failed} failed · {run.summary.errors} errors
        </button>)}
        {integrityDetail && <div>
          <h4>Run {integrityDetail.runId}</h4>
          {(integrityDetail.checks ?? []).map((check: any) => <div className="console-spec-item" key={check.code}>
            <span>{check.code}</span><span>{check.status} · {check.message}</span>
          </div>)}
        </div>}
      </div>
      <div className="console-card">
        <h3>Fetch Integrity Proof</h3>
        <form className="admin-form" onSubmit={fetchProof}>
          <div className="form-group">
            <label>Operation ID</label>
            <input value={operationId} onChange={(e) => setOperationId(e.target.value)} required />
          </div>
          <div className="form-group">
            <label>Participant ID</label>
            <input value={participantId} onChange={(e) => setParticipantId(e.target.value)} required />
          </div>
          <div className="form-group">
            <label>Scope From (ISO8601)</label>
            <input value={scopeFrom} onChange={(e) => setScopeFrom(e.target.value)} required />
          </div>
          <div className="form-group">
            <label>Scope To (ISO8601)</label>
            <input value={scopeTo} onChange={(e) => setScopeTo(e.target.value)} required />
          </div>
          <div className="form-actions">
            <Button type="submit" disabled={loading}>Fetch Proof</Button>
          </div>
        </form>
        {error && <InlineError message={error} />}
      </div>

      {proof && (
        <div className="console-card">
          <div style={{display: "flex", justifyContent: "space-between", alignItems: "center"}}>
            <h3>Integrity Proof Result</h3>
            <Button variant="secondary" onClick={handleVerify} disabled={verifying}>
              {verifying ? "Verifying..." : "Verify Proof Against Trusted State"}
            </Button>
          </div>

          {verifyError && <InlineError message={verifyError} />}
          {verifyResult && (
            <div className="console-notice-box" style={{marginBottom: "1.5rem", padding: "1rem", borderColor: verifyResult.valid ? "var(--success)" : "var(--error)", color: verifyResult.valid ? "var(--success)" : "var(--error)"}}>
              <h4 style={{margin: "0 0 0.5rem 0"}}>{verifyResult.valid ? "VERIFICATION SUCCESSFUL" : "VERIFICATION FAILED"}</h4>
              <p style={{margin: 0, fontSize: "0.85rem"}}>
                Reconstructed Root: <span style={{fontFamily: "monospace"}}>{verifyResult.reconstructedRoot}</span>
              </p>
            </div>
          )}

          <div className="console-spec-list">
            <div className="console-spec-item">
              <span className="console-spec-label">Leaf Hash</span>
              <span className="console-spec-val" style={{fontFamily: "monospace"}}>{proof.leafHashHex}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Generation</span>
              <span className="console-spec-val">{proof.generation}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Expected Root</span>
              <span className="console-spec-val" style={{fontFamily: "monospace"}}>{proof.expectedRootHex}</span>
            </div>
          </div>

          <h4>Bucket Path ({proof.bucketPath?.length || 0} nodes)</h4>
          {proof.bucketPath?.length > 0 ? (
            <ul style={{fontFamily: "monospace", fontSize: "0.85rem", listStyle: "none", paddingLeft: 0}}>
              {proof.bucketPath.map((p, i) => (
                <li key={i} style={{marginBottom: "0.25rem"}}>
                  <strong>{p.order}</strong>: {p.hashHex || "PROMOTED"}
                </li>
              ))}
            </ul>
          ) : (
            <p>No bucket path needed.</p>
          )}

          <h4>Global Path ({proof.globalPath?.length || 0} nodes)</h4>
          {proof.globalPath?.length > 0 ? (
            <ul style={{fontFamily: "monospace", fontSize: "0.85rem", listStyle: "none", paddingLeft: 0}}>
              {proof.globalPath.map((p, i) => (
                <li key={i} style={{marginBottom: "0.25rem"}}>
                  <strong>{p.order}</strong>: {p.hashHex || "PROMOTED"}
                </li>
              ))}
            </ul>
          ) : (
            <p>No global path needed.</p>
          )}

          <h4>Generation Metrics</h4>
          <div className="console-spec-list">
            <div className="console-spec-item">
              <span className="console-spec-label">Duration (ns)</span>
              <span className="console-spec-val">{proof.generationMetrics.durationNs}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Bucket Siblings</span>
              <span className="console-spec-val">{proof.generationMetrics.bucketSiblingCount}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Global Siblings</span>
              <span className="console-spec-val">{proof.generationMetrics.globalSiblingCount}</span>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// 7. Chaos View (Real M2 Chaos Controller)
// ---------------------------------------------------------------------------

function ConsoleChaosView({ token, liveRevision }: { token: string; liveRevision: number }) {
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

  async function runCorruptionFixture() {
    setError(""); setFeedback("");
    try {
      const result = await api.opsCorruptionFixture(token);
      setFeedback(`Isolated simulation: baseline ${result.baselineIntegrity.status}; after fixed ledger mutation ${result.corruptedIntegrity.status}; Merkle root mismatch ${result.merkleRootMismatch}; reconciliation discrepancies ${result.reconciliationDiscrepancies}.`);
    } catch (err) { setError(err instanceof Error ? err.message : "Corruption fixture unavailable. Enable TX_SIMULATION_MODE=true on the API."); }
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
        <p>Isolated simulation only. The fixed test record is discarded after verification; production ledger data is never changed. Requires TX_SIMULATION_MODE=true on the API.</p>
        <Button variant="secondary" onClick={runCorruptionFixture}>Run corruption detection</Button>
      </div>

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

function ConsoleActivityView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [events, setEvents] = useState<ActivityEvent[]>([]);
  const [nextOffset, setNextOffset] = useState<number | undefined>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  function loadEvents(offset = 0, append = false) {
    setLoading(true);
    setError("");
    api.opsActivity(token, 50, offset)
      .then((page) => {
        setEvents((current) => append ? [...current, ...(page.items ?? [])] : (page.items ?? []));
        setNextOffset(page.nextOffset);
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load operational activity.");
      })
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    loadEvents(0, false);
  }, [token, liveRevision]);

  return (
    <>
      <PageHeader
        eyebrow="Audit Feed"
        title="Operational Activity Log"
        description="Durable circuit, chaos, reconciliation, integrity, and routing facts in deterministic time order."
        action={
          <Button variant="secondary" onClick={() => loadEvents(0, false)}>
            <ConsoleIcon name="refresh" size={14} />
            Refresh
          </Button>
        }
      />

      {error && <InlineError message={error} />}

      {loading && events.length === 0 ? (
        <ConsoleSkeleton />
      ) : events.length === 0 ? (
        <div className="console-card">
          <span className="console-card-kicker">Activity Log</span>
          <p className="console-meta-text">No durable operational activity has been recorded.</p>
        </div>
      ) : (
        <section className="section-block">
          <div className="console-table-wrap">
            <table className="console-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Category</th>
                  <th>Event</th>
                  <th>Target</th>
                  <th>Summary</th>
                  <th>Severity</th>
                </tr>
              </thead>
              <tbody>
                {events.map((ev) => (
                  <tr key={ev.id}>
                    <td className="mono">{formatIso(ev.occurredAt)}</td>
                    <td><span className="console-pill console-pill-neutral">{ev.category}</span></td>
                    <td>{ev.title}<div className="mono" style={{ fontSize: ".68rem" }}>{ev.eventType}</div></td>
                    <td className="mono" style={{ fontWeight: 600 }}>{ev.targetId || "—"}</td>
                    <td>{ev.summary}</td>
                    <td><span className={`console-pill ${ev.severity === "ERROR" ? "console-pill-error" : ev.severity === "WARNING" ? "console-pill-warning" : "console-pill-success"}`}>{ev.severity}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {nextOffset !== undefined && <Button variant="secondary" disabled={loading} onClick={() => loadEvents(nextOffset, true)}>{loading ? "Loading..." : "Load more"}</Button>}
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
