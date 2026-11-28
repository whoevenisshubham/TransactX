import React from "react";
import { Link, useLocation } from "react-router-dom";
import { ThemeToggle } from "../theme/ThemeToggle";
import { SimulationBadge } from "../feedback/SimulationBadge";
import { Activity, ShieldCheck, Wallet, Store, Layers } from "lucide-react";

export function RootLayout({
  children,
  role = "CUSTOMER",
  userName: _userName = "Demo User",
  onLogout,
}: {
  children?: React.ReactNode;
  role?: string;
  userName?: string;
  onLogout?: () => void;
}) {
  const location = useLocation();

  const navItems = [
    { to: "/", label: "Overview", icon: <Layers className="w-4 h-4 shrink-0" aria-hidden="true" /> },
    { to: "/pay", label: "Send Money", icon: <Wallet className="w-4 h-4 shrink-0" aria-hidden="true" />, role: "CUSTOMER" },
    { to: "/transactions", label: "Activity", icon: <Activity className="w-4 h-4 shrink-0" aria-hidden="true" />, role: "CUSTOMER" },
    { to: "/merchant", label: "Merchant Station", icon: <Store className="w-4 h-4 shrink-0" aria-hidden="true" />, role: "MERCHANT" },
    { to: "/console/worklist", label: "Ops Console", icon: <ShieldCheck className="w-4 h-4 shrink-0" aria-hidden="true" />, opsOnly: true },
    { to: "/design-system", label: "Design System", icon: <Layers className="w-4 h-4 shrink-0" aria-hidden="true" /> },
  ];

  const filteredNav = navItems.filter((item) => {
    if (item.opsOnly) return role === "OPS_ADMIN" || role === "OPS_VIEWER";
    if (item.role) return role === item.role;
    return true;
  });

  return (
    <div className="min-h-screen flex flex-col bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors">
      <a href="#root-main" className="skip-link">
        Skip to content
      </a>

      {/* Top Header */}
      <header className="sticky top-0 z-40 w-full border-b border-[var(--border-hairline)] bg-[var(--bg-surface)]/95 backdrop-blur-md">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 h-16 flex items-center justify-between gap-3">
          {/* Brand + Nav */}
          <div className="flex items-center gap-4 lg:gap-6 min-w-0">
            <Link
              to="/"
              className="flex items-center gap-2.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] focus-visible:ring-offset-2 rounded-md shrink-0"
            >
              <div className="w-8 h-8 rounded-lg bg-[var(--action-primary-bg)] text-[var(--action-primary-fg)] flex items-center justify-center font-bold text-sm tracking-tight shadow-[var(--shadow-subtle)] shrink-0">
                TX
              </div>
              <div className="flex flex-col">
                <span className="font-semibold text-sm leading-tight tracking-tight text-[var(--text-primary)]">
                  TransactX
                </span>
                <span className="text-[10px] text-[var(--text-tertiary)] font-mono hidden sm:inline">
                  DESIGN SYSTEM
                </span>
              </div>
            </Link>

            <nav aria-label="Design system navigation" className="hidden lg:flex items-center gap-1">
              {filteredNav.map((item) => {
                const isActive = location.pathname === item.to;
                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium whitespace-nowrap transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] ${
                      isActive
                        ? "bg-[var(--bg-surface-raised)] text-[var(--text-primary)] font-semibold"
                        : "text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)]/60"
                    }`}
                  >
                    {item.icon}
                    <span>{item.label}</span>
                  </Link>
                );
              })}
            </nav>
          </div>

          {/* Right Header Controls */}
          <div className="flex items-center gap-2 sm:gap-3 shrink-0">
            <SimulationBadge className="hidden sm:inline-flex" />
            <ThemeToggle />
            {onLogout && (
              <button
                type="button"
                onClick={onLogout}
                className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] px-2.5 py-1.5 rounded-md border border-[var(--border-hairline)] hover:bg-[var(--bg-surface-raised)] whitespace-nowrap focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] cursor-pointer"
              >
                Sign out
              </button>
            )}
          </div>
        </div>
      </header>

      {/* Main Body */}
      <main id="root-main" className="flex-1 w-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 min-w-0">
        {children}
      </main>

      {/* Institutional Micro-Footer */}
      <footer className="border-t border-[var(--border-hairline)] bg-[var(--bg-surface)] py-6 text-xs text-[var(--text-secondary)]">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 flex flex-col sm:flex-row items-center justify-between gap-4 text-center sm:text-left">
          <p className="max-w-xl">
            © 2026 TransactX Foundation · Double-entry invariant verified · Non-custodial simulation testbed.
          </p>
          <div className="flex flex-wrap items-center justify-center sm:justify-start gap-4 font-mono text-[11px]">
            <span>ENGINE: v4.0</span>
            <span>COORDINATOR: LEASE-FENCED</span>
            <span className="text-[var(--status-success-fg)]">● NOMINAL</span>
          </div>
        </div>
      </footer>
    </div>
  );
}

export function PageContainer({
  title,
  subtitle,
  actions,
  children,
  className = "",
}: {
  title?: string;
  subtitle?: string;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={`space-y-6 ${className}`}>
      {(title || actions) && (
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 pb-4 border-b border-[var(--border-hairline)]">
          <div className="min-w-0">
            {title && (
              <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-[var(--text-primary)]">{title}</h1>
            )}
            {subtitle && <p className="text-xs sm:text-sm text-[var(--text-secondary)] mt-1">{subtitle}</p>}
          </div>
          {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
        </div>
      )}
      <div>{children}</div>
    </div>
  );
}
