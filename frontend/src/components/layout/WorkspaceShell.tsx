import React, { useRef, useState } from "react";
import { Menu, PanelLeftClose, PanelLeftOpen, LogOut, X } from "lucide-react";
import { Avatar, BrandMark, Button } from "../../components";
import { ThemeToggle } from "../theme/ThemeToggle";

export interface WorkspaceNavItem {
  id: string;
  label: string;
  active: boolean;
  icon: React.ReactNode;
  onSelect: () => void;
}

export interface WorkspaceNavGroup {
  id: string;
  title?: string;
  items: WorkspaceNavItem[];
}

export interface WorkspaceShellProps {
  role: "customer" | "merchant" | "operations";
  roleLabel: string;
  roleBadgeClassName?: string;
  shellClassName: string;
  desktopNavAriaLabel: string;
  mobileDialogLabel: string;
  menuButtonLabel?: string;
  defaultDesktopCollapsed?: boolean;
  navGroups: WorkspaceNavGroup[];
  bottomNavItems?: WorkspaceNavItem[];
  user: { name: string; secondary?: string } | null;
  onLogout: () => void;
  logoutAriaLabel?: string;
  children: React.ReactNode;
}

export function WorkspaceShell({
  role,
  roleLabel,
  roleBadgeClassName = "customer-tag",
  shellClassName,
  desktopNavAriaLabel,
  mobileDialogLabel,
  menuButtonLabel = "Open navigation",
  defaultDesktopCollapsed = false,
  navGroups,
  bottomNavItems,
  user,
  onLogout,
  logoutAriaLabel,
  children,
}: WorkspaceShellProps) {
  const [collapsed, setCollapsed] = useState(defaultDesktopCollapsed);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const menuButtonRef = useRef<HTMLButtonElement>(null);

  const hasBottomNav = role !== "operations" && Boolean(bottomNavItems && bottomNavItems.length > 0);
  const displayName = user?.name ?? (role === "merchant" ? "Merchant" : role === "operations" ? "Operator" : "Customer");

  function openDrawer() {
    dialogRef.current?.showModal();
  }

  function closeDrawer(restoreFocus = false) {
    dialogRef.current?.close();
    if (restoreFocus) {
      menuButtonRef.current?.focus();
    }
  }

  function selectNavItem(onSelect: () => void) {
    closeDrawer(false);
    onSelect();
  }

  return (
    <div
      className={`tx-shell product-shell ${shellClassName}`.trim()}
      data-role={role}
      data-collapsed={collapsed ? "true" : "false"}
      data-has-bottom-nav={hasBottomNav ? "true" : "false"}
    >
      <a href="#workspace-main" className="skip-link">
        Skip to content
      </a>

      {/* Desktop Collapsible / Expandable Sidebar */}
      <aside className="tx-sidebar sidebar" aria-label={desktopNavAriaLabel}>
        <div
          className={`flex items-center ${
            collapsed ? "flex-col gap-2" : "justify-between gap-2"
          } pb-4 border-b border-[var(--border-hairline)] w-full`}
        >
          <div className="flex items-center gap-2.5 min-w-0">
            <BrandMark />
            {!collapsed && (
              <div className="flex items-center gap-2 min-w-0 flex-wrap">
                <span className="font-semibold text-base tracking-tight text-[var(--text-primary)]">
                  TransactX
                </span>
                <span className={`${roleBadgeClassName} !m-0 whitespace-nowrap`}>{roleLabel}</span>
              </div>
            )}
          </div>

          <button
            type="button"
            onClick={() => setCollapsed((prev) => !prev)}
            aria-label={collapsed ? "Expand navigation sidebar" : "Collapse navigation sidebar"}
            aria-expanded={!collapsed}
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            className="inline-flex items-center justify-center w-8 h-8 rounded-md text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] shrink-0 cursor-pointer"
          >
            {collapsed ? (
              <PanelLeftOpen className="w-4 h-4" aria-hidden="true" />
            ) : (
              <PanelLeftClose className="w-4 h-4" aria-hidden="true" />
            )}
          </button>
        </div>

        <nav aria-label={desktopNavAriaLabel} className="flex-1 py-4 space-y-4 w-full">
          {navGroups.map((group) => (
            <div key={group.id} className="space-y-1">
              {group.title && !collapsed && (
                <div className="px-2.5 pb-1 text-[11px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]">
                  {group.title}
                </div>
              )}
              {group.items.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={item.onSelect}
                  aria-current={item.active ? "page" : undefined}
                  aria-label={item.label}
                  title={collapsed ? item.label : undefined}
                  className={`tx-nav-item nav-item ${item.active ? "is-active" : ""}`}
                >
                  <span className="shrink-0 inline-flex items-center justify-center">{item.icon}</span>
                  <span className={collapsed ? "sr-only" : "truncate"}>{item.label}</span>
                </button>
              ))}
            </div>
          ))}
        </nav>

        <div
          className={`pt-4 border-t border-[var(--border-hairline)] flex flex-col gap-3 w-full ${
            collapsed ? "items-center" : ""
          }`}
        >
          {collapsed ? <ThemeToggle iconOnly /> : <ThemeToggle fullWidth />}

          <div
            className={`flex items-center ${collapsed ? "justify-center" : "gap-2.5"} min-w-0`}
            title={collapsed ? displayName : undefined}
          >
            <Avatar name={displayName} size="md" />
            {!collapsed && (
              <div className="min-w-0 flex-1">
                <div className="text-xs font-semibold text-[var(--text-primary)] truncate">
                  {displayName}
                </div>
                {user?.secondary && (
                  <div className="text-[11px] font-mono text-[var(--text-tertiary)] truncate">
                    {user.secondary}
                  </div>
                )}
              </div>
            )}
          </div>

          <button
            type="button"
            onClick={onLogout}
            aria-label={logoutAriaLabel ?? (collapsed ? "Log out" : undefined)}
            title={collapsed ? "Log out" : undefined}
            className={`inline-flex items-center ${
              collapsed ? "justify-center w-9 h-9" : "gap-2 px-2.5 py-1.5 w-full"
            } rounded-md text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--status-danger-fg)] hover:bg-[var(--status-danger-bg)]/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] transition-colors cursor-pointer`}
          >
            <LogOut className="w-3.5 h-3.5 shrink-0" aria-hidden="true" />
            {!collapsed && <span>Log out</span>}
          </button>
        </div>
      </aside>

      {/* Main Content Area + Tablet/Mobile Sticky Top Bar */}
      <main id="workspace-main" className="tx-main main-content">
        <header className="tx-mobile-header mobile-header">
          <div className="flex items-center gap-2.5 min-w-0">
            <button
              ref={menuButtonRef}
              type="button"
              onClick={openDrawer}
              aria-label={menuButtonLabel}
              className="operator-menu-button inline-flex items-center justify-center w-9 h-9 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] text-[var(--text-primary)] hover:border-[var(--border-active)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] shrink-0 cursor-pointer"
            >
              <Menu className="w-4 h-4" aria-hidden="true" />
            </button>

            <div className="flex items-center gap-2 min-w-0 shrink-0">
              <BrandMark />
              <span className="font-semibold text-sm tracking-tight text-[var(--text-primary)] whitespace-nowrap shrink-0">
                TransactX
              </span>
              <span className={`${roleBadgeClassName} !hidden min-[430px]:!inline-flex !m-0 whitespace-nowrap shrink-0`}>
                {roleLabel}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <ThemeToggle compact />
            <button
              type="button"
              onClick={onLogout}
              aria-label={`Log out ${displayName}`}
              title="Log out"
              className="inline-flex items-center justify-center rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] cursor-pointer shrink-0"
            >
              <Avatar name={displayName} size="sm" />
            </button>
          </div>
        </header>

        <div className="content-wrap">{children}</div>
      </main>

      {/* Phone-only (< 640px) Bottom Nav for Customer & Merchant */}
      {hasBottomNav && bottomNavItems && (
        <nav className="tx-bottom-nav" aria-label={`Mobile ${role} navigation`}>
          {bottomNavItems.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={item.onSelect}
              aria-current={item.active ? "page" : undefined}
              className={`tx-bottom-nav-item ${item.active ? "is-active" : ""}`}
            >
              <span className="shrink-0">{item.icon}</span>
              <span className="truncate max-w-full px-1">{item.label}</span>
            </button>
          ))}
        </nav>
      )}

      {/* Tablet & Mobile Slide-out Navigation Dialog */}
      <dialog
        ref={dialogRef}
        className="tx-nav-dialog operator-navigation-dialog"
        aria-label={mobileDialogLabel}
        onCancel={(event) => {
          event.preventDefault();
          closeDrawer(true);
        }}
      >
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-2 pb-3 border-b border-[var(--border-hairline)]">
            <div className="flex items-center gap-2 min-w-0">
              <BrandMark />
              <span className="font-semibold text-sm text-[var(--text-primary)]">TransactX</span>
              <span className={`${roleBadgeClassName} !m-0 whitespace-nowrap`}>{roleLabel}</span>
            </div>
            <button
              type="button"
              onClick={() => closeDrawer(true)}
              aria-label="Close navigation"
              className="inline-flex items-center justify-center w-8 h-8 rounded-md border border-[var(--border-hairline)] text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] cursor-pointer"
            >
              <X className="w-4 h-4" aria-hidden="true" />
            </button>
          </div>

          <nav aria-label={mobileDialogLabel} className="space-y-4">
            {navGroups.map((group) => (
              <div key={group.id} className="space-y-1">
                {group.title && (
                  <div className="px-2 pb-1 text-[11px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]">
                    {group.title}
                  </div>
                )}
                {group.items.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => selectNavItem(item.onSelect)}
                    aria-current={item.active ? "page" : undefined}
                    className={`tx-nav-item nav-item ${item.active ? "is-active" : ""}`}
                  >
                    <span className="shrink-0 inline-flex items-center justify-center">{item.icon}</span>
                    <span>{item.label}</span>
                  </button>
                ))}
              </div>
            ))}
          </nav>

          <div className="pt-3 border-t border-[var(--border-hairline)] space-y-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-medium text-[var(--text-secondary)]">Theme</span>
              <ThemeToggle />
            </div>

            <div className="flex items-center gap-2.5 min-w-0">
              <Avatar name={displayName} size="md" />
              <div className="min-w-0 flex-1">
                <div className="text-xs font-semibold text-[var(--text-primary)] truncate">
                  {displayName}
                </div>
                {user?.secondary && (
                  <div className="text-[11px] font-mono text-[var(--text-tertiary)] truncate">
                    {user.secondary}
                  </div>
                )}
              </div>
            </div>

            <Button
              variant="secondary"
              size="sm"
              className="w-full"
              onClick={() => {
                closeDrawer(false);
                onLogout();
              }}
            >
              Log out
            </Button>
          </div>
        </div>
      </dialog>
    </div>
  );
}
