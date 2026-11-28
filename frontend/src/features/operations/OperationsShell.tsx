import React, { useEffect, useState } from "react";
import { api } from "../../api";
import { WorkspaceShell } from "../../components/layout/WorkspaceShell";
import { DotSentinel } from "../../components/effects";
import { subscribeOperationalEvents, type StreamStatus } from "../../operationalEvents";
import type { ConsoleView, User } from "./types";
import {
  CONSOLE_NAV_GROUPS,
  consoleViewPath,
  permittedConsoleView,
  readConsoleView,
} from "./navigation";
import { ConsoleIcon } from "./ConsoleIcon";
import { AdminOverviewPage } from "../../ops/AdminOverviewPage";
import { AdminHealthPage } from "../../ops/AdminHealthPage";
import { WorklistPage } from "../../ops/WorklistPage";
import { PaymentEvidencePage } from "../../ops/PaymentEvidencePage";
import { AnalyticsPage } from "../../ops/AnalyticsPage";
import { InvestigationPage } from "../../ops/InvestigationPage";
import { RoutingView } from "./RoutingView";
import { ReconciliationView } from "./ReconciliationView";
import { MerkleView } from "./MerkleView";
import { IntegrityView } from "./IntegrityView";
import { ChaosView } from "./ChaosView";
import { ActivityView } from "./ActivityView";

export function OperationsShell({
  token,
  user,
  onLogout,
}: {
  token: string;
  user: User;
  onLogout: () => void;
}) {
  const [view, setView] = useState<ConsoleView>(() =>
    permittedConsoleView(readConsoleView(), user.role)
  );
  const [routeRevision, setRouteRevision] = useState(0);
  const [liveRevision, setLiveRevision] = useState(0);
  const [streamStatus, setStreamStatus] = useState<StreamStatus>("connecting");
  const [simulationMode, setSimulationMode] = useState(false);

  useEffect(() => {
    const handle = () => {
      const requested = readConsoleView();
      const permitted = permittedConsoleView(requested, user.role);
      if (requested !== permitted) {
        window.history.replaceState({}, "", consoleViewPath(permitted));
      }
      setView(permitted);
      setRouteRevision((revision) => revision + 1);
    };
    window.addEventListener("popstate", handle);
    return () => window.removeEventListener("popstate", handle);
  }, [user.role]);

  useEffect(() => {
    if (
      user.role === "OPS_VIEWER" &&
      permittedConsoleView(readConsoleView(), user.role) !== readConsoleView()
    ) {
      window.history.replaceState({}, "", "/console/worklist");
    }
  }, [user.role]);

  useEffect(() => {
    return subscribeOperationalEvents(
      token,
      () => setLiveRevision((revision) => revision + 1),
      setStreamStatus,
      { onUnauthorized: onLogout }
    );
  }, [token, onLogout]);

  useEffect(() => {
    let active = true;
    api
      .opsRuntime(token)
      .then((runtime) => {
        if (active) setSimulationMode(runtime.simulationMode);
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [token]);

  function navigate(nextView: ConsoleView) {
    nextView = permittedConsoleView(nextView, user.role);
    const path = consoleViewPath(nextView);
    window.history.pushState({}, "", path);
    setView(nextView);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function navigatePath(path: string) {
    const target = new URL(path, window.location.origin);
    if (target.origin !== window.location.origin) return;
    if (
      user.role === "OPS_VIEWER" &&
      !/^\/console\/(worklist|analytics|payments\/[0-9a-f-]{36}|investigations\/[0-9a-f-]{36})$/i.test(
        target.pathname
      )
    ) {
      return;
    }
    const from =
      path.startsWith("/console/payments/") && window.location.pathname === "/console/worklist"
        ? { from: `${window.location.pathname}${window.location.search}` }
        : {};
    window.history.pushState(from, "", path);
    setView(readConsoleView(target.pathname));
    setRouteRevision((revision) => revision + 1);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  const navGroups = CONSOLE_NAV_GROUPS.filter(
    (group) => !group.admin || user.role === "OPS_ADMIN"
  ).map((group) => ({
    id: group.title,
    title: group.title,
    items: group.links.map((link) => ({
      id: link.view,
      label: link.label,
      active:
        view === link.view ||
        (link.view === "c-worklist" && ["c-payment-evidence", "c-investigation"].includes(view)),
      icon: <ConsoleIcon name={link.icon} />,
      onSelect: () => navigate(link.view),
    })),
  }));

  return (
    <WorkspaceShell
      role="operations"
      roleLabel={user.role === "OPS_VIEWER" ? "Read-only viewer" : "Private operator"}
      roleBadgeClassName="console-badge-role"
      shellClassName="console-shell reset-workspace reset-operations reset-expanded"
      desktopNavAriaLabel="Console navigation"
      mobileDialogLabel="Operations navigation"
      menuButtonLabel="Open operations navigation"
      defaultDesktopCollapsed={true}
      navGroups={navGroups}
      user={{ name: user.name, secondary: user.paymentIdentifier }}
      onLogout={onLogout}
    >
      <div className="operator-status-bar flex flex-wrap items-center gap-2 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3.5 py-2.5 mb-4">
        <span className="operator-breadcrumb text-xs font-mono font-semibold text-[var(--text-secondary)] mr-auto">
          Operations /{" "}
          {view === "c-payment-evidence"
            ? "Payment journey"
            : view === "c-investigation"
            ? "Saved investigation"
            : view.replace("c-", "")}
        </span>
        {simulationMode && <span className="console-pill console-pill-warning">SIMULATION MODE</span>}
        <span
          className={`console-pill inline-flex items-center gap-1.5 ${
            streamStatus === "connected"
              ? "console-pill-success"
              : streamStatus === "connecting"
              ? "console-pill-warning"
              : "console-pill-error"
          }`}
        >
          <DotSentinel
            state={
              streamStatus === "connected"
                ? "success"
                : streamStatus === "connecting"
                ? "probing"
                : "error"
            }
          />
          EVENT STREAM {streamStatus.toUpperCase()}
        </span>
      </div>

      <details className="operator-guide mb-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 px-4 py-2.5 text-xs text-[var(--text-secondary)]">
        <summary className="cursor-pointer font-semibold text-[var(--text-primary)] select-none">
          Start here · Follow a payment in three steps
        </summary>
        <ol className="mt-2.5 list-decimal pl-5 space-y-1">
          <li>Open the worklist. Use Completed to find a settled payment, or inspect one awaiting confirmation.</li>
          <li>Open its journey. Compare central state, bank observations and recovery records; check their source times.</li>
          <li>Ask an investigation question. Follow citations to the evidence and keep missing observations distinct from failure.</li>
        </ol>
        <p className="mt-2 text-[11px] font-mono text-[var(--text-tertiary)] m-0">
          Event stream connectivity is a refresh hint, not a bank-health verdict.
        </p>
      </details>

      {view === "c-overview" && user.role === "OPS_ADMIN" && (
        <AdminOverviewPage token={token} onNavigate={navigate} liveRevision={liveRevision} />
      )}
      {view === "c-worklist" && (
        <WorklistPage token={token} liveRevision={liveRevision} onNavigate={navigatePath} />
      )}
      {view === "c-payment-evidence" && (
        <PaymentEvidencePage
          key={`${routeRevision}:${window.location.pathname}`}
          paymentId={window.location.pathname.split("/")[3] ?? ""}
          token={token}
          isAdmin={user.role === "OPS_ADMIN"}
          liveRevision={liveRevision}
          onNavigate={navigatePath}
        />
      )}
      {view === "c-analytics" && (
        <AnalyticsPage token={token} liveRevision={liveRevision} />
      )}
      {view === "c-investigation" && (
        <InvestigationPage
          key={`${routeRevision}:${window.location.pathname}`}
          id={window.location.pathname.split("/")[3] ?? ""}
          token={token}
          onNavigate={navigatePath}
        />
      )}
      {view === "c-health" && user.role === "OPS_ADMIN" && (
        <AdminHealthPage token={token} liveRevision={liveRevision} />
      )}
      {view === "c-routing" && user.role === "OPS_ADMIN" && (
        <RoutingView token={token} liveRevision={liveRevision} />
      )}
      {view === "c-reconciliation" && user.role === "OPS_ADMIN" && (
        <ReconciliationView token={token} liveRevision={liveRevision} />
      )}
      {view === "c-merkle" && user.role === "OPS_ADMIN" && (
        <MerkleView token={token} />
      )}
      {view === "c-integrity" && user.role === "OPS_ADMIN" && (
        <IntegrityView token={token} liveRevision={liveRevision} />
      )}
      {view === "c-chaos" && user.role === "OPS_ADMIN" && (
        <ChaosView token={token} liveRevision={liveRevision} />
      )}
      {view === "c-activity" && user.role === "OPS_ADMIN" && (
        <ActivityView token={token} liveRevision={liveRevision} />
      )}
    </WorkspaceShell>
  );
}
