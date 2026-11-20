import React, { useEffect, useRef, useState } from "react";
import { api } from "../../api";
import { Avatar, BrandMark, Button } from "../../components";
import { subscribeOperationalEvents, type StreamStatus } from "../../operationalEvents";
import { ThemeSelect } from "../../theme";
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
  const navigationDialog = useRef<HTMLDialogElement>(null);
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
    navigationDialog.current?.close();
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

  return (
    <div className="product-shell console-shell reset-workspace reset-operations reset-expanded">
      <ConsoleSidebar view={view} user={user} onNavigate={navigate} onLogout={onLogout} />
      <main className="main-content">
        <ConsoleMobileHeader
          user={user}
          onLogout={onLogout}
          onMenu={() => navigationDialog.current?.showModal()}
        />
        <div className="content-wrap">
          <div className="operator-status-bar">
            <span className="operator-breadcrumb">
              Operations /{" "}
              {view === "c-payment-evidence"
                ? "Payment journey"
                : view === "c-investigation"
                ? "Saved investigation"
                : view.replace("c-", "")}
            </span>
            {simulationMode && <span className="console-pill console-pill-warning">SIMULATION MODE</span>}
            <span
              className={`console-pill ${
                streamStatus === "connected"
                  ? "console-pill-success"
                  : streamStatus === "connecting"
                  ? "console-pill-warning"
                  : "console-pill-error"
              }`}
            >
              EVENT STREAM {streamStatus.toUpperCase()}
            </span>
          </div>

          <details className="operator-guide">
            <summary>Start here · Follow a payment in three steps</summary>
            <ol>
              <li>Open the worklist. Use Completed to find a settled payment, or inspect one awaiting confirmation.</li>
              <li>Open its journey. Compare central state, bank observations and recovery records; check their source times.</li>
              <li>Ask an investigation question. Follow citations to the evidence and keep missing observations distinct from failure.</li>
            </ol>
            <p>Event stream connectivity is a refresh hint, not a bank-health verdict.</p>
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
        </div>
      </main>

      <dialog
        ref={navigationDialog}
        className="operator-navigation-dialog"
        aria-label="Operations navigation"
      >
        <Button
          variant="quiet"
          className="operator-close-menu"
          onClick={() => navigationDialog.current?.close()}
        >
          Close navigation
        </Button>
        <ConsoleSidebar
          view={view}
          user={user}
          onNavigate={navigate}
          onLogout={() => {
            navigationDialog.current?.close();
            onLogout();
          }}
        />
      </dialog>
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
        <span className="console-badge-role">
          {user.role === "OPS_VIEWER" ? "Read-only viewer" : "Private operator"}
        </span>
      </div>
      <div className="sidebar-rule" />
      <nav aria-label="Console navigation">
        {CONSOLE_NAV_GROUPS.filter((group) => !group.admin || user.role === "OPS_ADMIN").map((group) => (
          <div className="operator-nav-group" key={group.title}>
            <span>{group.title}</span>
            {group.links.map((link) => (
              <ConsoleNavItem
                key={link.view}
                view={link.view}
                active={
                  view === link.view ||
                  (link.view === "c-worklist" && ["c-payment-evidence", "c-investigation"].includes(view))
                }
                label={link.label}
                icon={link.icon}
                onClick={onNavigate}
              />
            ))}
          </div>
        ))}
      </nav>
      <div className="sidebar-bottom">
        <ThemeSelect />
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
  icon: import("./types").ConsoleIconName;
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

function ConsoleMobileHeader({
  user,
  onLogout,
  onMenu,
}: {
  user: User;
  onLogout: () => void;
  onMenu: () => void;
}) {
  return (
    <header className="mobile-header console-mobile-header">
      <button
        type="button"
        className="operator-menu-button"
        aria-label="Open operations navigation"
        onClick={onMenu}
      >
        ☰
      </button>
      <div className="mobile-brand">
        <BrandMark />
        <span>TransactX</span>
        <span className="console-badge-role">
          {user.role === "OPS_VIEWER" ? "Read-only viewer" : "Private operator"}
        </span>
      </div>
      <ThemeSelect />
      <button
        type="button"
        className="mobile-user"
        onClick={onLogout}
        aria-label={`Log out ${user.name}`}
      >
        <Avatar name={user.name} size="sm" />
      </button>
    </header>
  );
}
