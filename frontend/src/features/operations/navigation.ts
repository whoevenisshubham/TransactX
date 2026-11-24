import type { ConsoleView, NavigationGroup } from "./types";

export function readConsoleView(
  pathname = typeof window !== "undefined" ? window.location.pathname : "/console"
): ConsoleView {
  if (pathname === "/console/worklist") return "c-worklist";
  if (/^\/console\/payments\/[0-9a-f-]{36}$/i.test(pathname)) return "c-payment-evidence";
  if (pathname === "/console/analytics") return "c-analytics";
  if (/^\/console\/investigations\/[0-9a-f-]{36}$/i.test(pathname)) return "c-investigation";
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
    case "c-worklist":
      return "/console/worklist";
    case "c-payment-evidence":
      return "/console/worklist";
    case "c-analytics":
      return "/console/analytics";
    case "c-investigation":
      return "/console/worklist";
    case "c-health":
      return "/console/health";
    case "c-routing":
      return "/console/routing";
    case "c-reconciliation":
      return "/console/reconciliation";
    case "c-merkle":
      return "/console/merkle";
    case "c-integrity":
      return "/console/integrity";
    case "c-chaos":
      return "/console/chaos";
    case "c-activity":
      return "/console/activity";
    default:
      return "/console";
  }
}

export function permittedConsoleView(view: ConsoleView, role: string): ConsoleView {
  if (role !== "OPS_VIEWER") return view;
  return view === "c-payment-evidence" ||
    view === "c-worklist" ||
    view === "c-analytics" ||
    view === "c-investigation"
    ? view
    : "c-worklist";
}

export const CONSOLE_NAV_GROUPS: NavigationGroup[] = [
  {
    title: "Monitor",
    admin: true,
    links: [
      { view: "c-overview", label: "Overview", icon: "overview" },
      { view: "c-health", label: "Bank health", icon: "health" },
      { view: "c-routing", label: "Routing", icon: "routing" },
    ],
  },
  {
    title: "Investigate",
    admin: false,
    links: [
      { view: "c-worklist", label: "Worklist", icon: "activity" },
      { view: "c-analytics", label: "Analytics", icon: "pulse" },
    ],
  },
  {
    title: "Verify",
    admin: true,
    links: [
      { view: "c-reconciliation", label: "Reconciliation", icon: "reconciliation" },
      { view: "c-merkle", label: "Merkle proofs", icon: "merkle" },
      { view: "c-integrity", label: "Integrity", icon: "integrity" },
    ],
  },
  {
    title: "System",
    admin: true,
    links: [
      { view: "c-activity", label: "Activity", icon: "activity" },
      { view: "c-chaos", label: "Controlled faults", icon: "chaos" },
    ],
  },
];
