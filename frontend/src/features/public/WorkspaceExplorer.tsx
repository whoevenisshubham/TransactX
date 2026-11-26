import React, { useState } from "react";
import { ArrowRight, Send, QrCode, ShieldCheck, CheckCircle2, Clock } from "lucide-react";
import { BrandMark } from "../../components";

export const roles = [
  {
    id: "customer",
    name: "Customer",
    title: "Your money. A clearer view.",
    text: "Review a recipient, send a simulated payment and keep its receipt. Your account stays your own.",
    action: "Open customer demo",
  },
  {
    id: "merchant",
    name: "Merchant",
    title: "A desk built for receiving.",
    text: "Share your payment identity, follow incoming payments and separate completed receipts from uncertain outcomes.",
    action: "Open merchant demo",
  },
  {
    id: "operator",
    name: "Operations",
    title: "Follow the facts, not the guesswork.",
    text: "Read a payment’s original path, compare independent bank observations and open a read-only investigation with sources.",
    action: "Open operations demo",
  },
];

function RolePreview({ role }: { role: string }) {
  if (role === "customer") {
    return (
      <div className="space-y-4">
        <div className="rounded-xl bg-[#0f172a] p-5 text-[#f8fafc] space-y-4">
          <div className="flex items-center justify-between text-[11px] font-mono text-[#94a3b8]">
            <span>PERSONAL ACCOUNT · BANK A</span>
            <span className="inline-flex items-center gap-1.5 text-[#34d399]">
              <span className="h-1.5 w-1.5 rounded-full bg-[#34d399]" />
              Active
            </span>
          </div>
          <div>
            <div className="text-[11px] font-mono uppercase tracking-wider text-[#94a3b8]">
              Available balance
            </div>
            <div className="mt-1 font-mono text-3xl font-bold tracking-tight tabular-nums">
              ₹9,852.50
            </div>
          </div>
          <div className="flex items-center justify-between border-t border-white/10 pt-3 text-xs text-[#cbd5e1]">
            <span className="font-mono">alice@fixture</span>
            <span className="inline-flex items-center gap-1 rounded-md bg-white/10 px-2.5 py-1 text-[11px] font-semibold text-white">
              <Send className="w-3 h-3" aria-hidden="true" />
              Send money
            </span>
          </div>
        </div>

        <div className="space-y-2">
          <div className="flex items-center justify-between rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3.5 py-2.5 text-xs">
            <div>
              <div className="font-semibold text-[var(--text-primary)]">Demo Store</div>
              <div className="text-[11px] font-mono text-[var(--text-tertiary)]">Sent · Bank A → Bank B</div>
            </div>
            <div className="text-right">
              <div className="font-mono font-semibold text-[var(--text-primary)] tabular-nums">-₹125.00</div>
              <div className="text-[10px] font-mono text-[var(--status-success-fg)]">Completed</div>
            </div>
          </div>
          <div className="flex items-center justify-between rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3.5 py-2.5 text-xs">
            <div>
              <div className="font-semibold text-[var(--text-primary)]">Bob Counterparty</div>
              <div className="text-[11px] font-mono text-[var(--text-tertiary)]">Received · Bank B → Bank A</div>
            </div>
            <div className="text-right">
              <div className="font-mono font-semibold text-[var(--status-success-fg)] tabular-nums">+₹250.00</div>
              <div className="text-[10px] font-mono text-[var(--status-warning-fg)]">Confirming</div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  if (role === "merchant") {
    return (
      <div className="space-y-4">
        <div className="flex items-center justify-between rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-4">
          <div className="space-y-1">
            <span className="text-[11px] font-mono uppercase tracking-wider text-[var(--action-accent-bg)] font-semibold">
              Receiving desk · Bank B
            </span>
            <div className="font-mono text-2xl font-bold text-[var(--text-primary)] tabular-nums">
              ₹4,287.50
            </div>
            <div className="text-xs font-mono text-[var(--text-tertiary)]">store@fixture</div>
          </div>
          <div className="flex h-14 w-14 items-center justify-center rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] text-[var(--text-primary)]">
            <QrCode className="w-7 h-7" aria-hidden="true" />
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3 text-xs">
          <div className="rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-3">
            <div className="flex items-center gap-1.5 text-[var(--status-success-fg)] font-semibold">
              <CheckCircle2 className="w-3.5 h-3.5" aria-hidden="true" />
              <span>Settled</span>
            </div>
            <div className="mt-1 font-mono text-base font-bold text-[var(--text-primary)] tabular-nums">
              ₹125.00
            </div>
          </div>
          <div className="rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-3">
            <div className="flex items-center gap-1.5 text-[var(--status-warning-fg)] font-semibold">
              <Clock className="w-3.5 h-3.5" aria-hidden="true" />
              <span>Awaiting</span>
            </div>
            <div className="mt-1 font-mono text-base font-bold text-[var(--text-primary)] tabular-nums">
              12
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3.5 py-2.5 text-xs">
        <div className="flex items-center gap-2 font-mono font-semibold text-[var(--text-primary)]">
          <ShieldCheck className="w-4 h-4 text-[var(--action-accent-bg)]" aria-hidden="true" />
          <span>OPS_VIEWER · Read-only</span>
        </div>
        <span className="rounded-full border border-[var(--status-warning-border)] bg-[var(--status-warning-bg)] px-2 py-0.5 text-[10px] font-mono font-semibold text-[var(--status-warning-fg)]">
          PENDING_RECONCILIATION
        </span>
      </div>

      <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-4 space-y-2.5 text-xs">
        <div className="flex items-center justify-between font-mono text-[11px] text-[var(--text-tertiary)]">
          <span>Payment Journey · 11111111…</span>
          <span>2 recovery probes</span>
        </div>
        <div className="grid grid-cols-2 gap-2.5 pt-1">
          <div className="rounded-lg bg-[var(--bg-surface-raised)] p-2.5">
            <div className="text-[10px] font-mono uppercase text-[var(--text-tertiary)]">Bank A</div>
            <div className="mt-0.5 font-semibold text-[var(--text-primary)]">Hold reported</div>
          </div>
          <div className="rounded-lg bg-[var(--bg-surface-raised)] p-2.5">
            <div className="text-[10px] font-mono uppercase text-[var(--text-tertiary)]">Bank B</div>
            <div className="mt-0.5 font-semibold text-[var(--status-warning-fg)]">Reply unavailable</div>
          </div>
        </div>
        <div className="border-t border-[var(--border-hairline)] pt-2 text-[11px] text-[var(--text-secondary)]">
          Central outcome and participant observations stay distinct with source timestamps.
        </div>
      </div>
    </div>
  );
}

export function WorkspaceExplorer() {
  const [selected, setSelected] = useState(0);
  const role = roles[selected];

  return (
    <section id="workspaces" className="border-t border-[var(--border-hairline)] bg-[var(--bg-surface)]/50">
      <div className="mx-auto max-w-7xl px-4 py-14 sm:px-6 lg:px-8 lg:py-20 space-y-10">
        <div className="max-w-2xl space-y-2">
          <p className="text-xs font-mono font-semibold text-[var(--action-accent-bg)] m-0">
            One network. Your perspective.
          </p>
          <h2 className="text-3xl font-bold tracking-tight text-[var(--text-primary)] sm:text-4xl m-0">
            Choose your view. Follow a payment.
          </h2>
        </div>

        <div className="grid grid-cols-1 gap-8 lg:grid-cols-12 lg:items-start">
          {/* Left Tabs & Role Description */}
          <div className="lg:col-span-5 space-y-6">
            <div
              role="tablist"
              aria-label="Explore workspaces"
              className="inline-flex w-full rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] p-1"
            >
              {roles.map((item, index) => (
                <button
                  key={item.id}
                  role="tab"
                  type="button"
                  id={`role-tab-${item.id}`}
                  aria-selected={selected === index}
                  aria-controls="role-preview-panel"
                  tabIndex={selected === index ? 0 : -1}
                  onClick={() => setSelected(index)}
                  onKeyDown={(event) => {
                    if (["ArrowRight", "ArrowLeft", "Home", "End"].includes(event.key)) {
                      event.preventDefault();
                      const next =
                        event.key === "Home"
                          ? 0
                          : event.key === "End"
                          ? 2
                          : (selected + (event.key === "ArrowRight" ? 1 : 2)) % 3;
                      setSelected(next);
                      document.getElementById(`role-tab-${roles[next].id}`)?.focus();
                    }
                  }}
                  className={`flex-1 rounded-lg px-3 py-2 text-xs font-semibold transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] ${
                    selected === index
                      ? "bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[var(--shadow-subtle)]"
                      : "text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                  }`}
                >
                  {item.name}
                </button>
              ))}
            </div>

            <div className="space-y-4">
              <h3 className="text-2xl font-bold tracking-tight text-[var(--text-primary)] m-0">
                {role.title}
              </h3>
              <p className="text-sm leading-relaxed text-[var(--text-secondary)] m-0">
                {role.text}
              </p>
              <a
                className="inline-flex items-center gap-2 rounded-lg bg-[var(--action-primary-bg)] px-4 py-2.5 text-xs font-semibold !text-[var(--action-primary-fg)] hover:opacity-90 transition-opacity no-underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
                href={`/demo?role=${role.id}`}
              >
                <span>{role.action}</span>
                <ArrowRight className="w-3.5 h-3.5" aria-hidden="true" />
              </a>
            </div>
          </div>

          {/* Right Live Preview Panel */}
          <div
            className="lg:col-span-7 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 p-5 sm:p-6 shadow-[var(--shadow-subtle)] space-y-4"
            role="tabpanel"
            id="role-preview-panel"
            aria-labelledby={`role-tab-${role.id}`}
          >
            <div className="flex items-center justify-between border-b border-[var(--border-hairline)] pb-3 text-[11px] font-mono text-[var(--text-tertiary)]">
              <span className="inline-flex items-center gap-2 font-semibold text-[var(--text-primary)]">
                <BrandMark />
                <span>TransactX · {role.name}</span>
              </span>
              <span>Live-rendered preview · synthetic fixtures</span>
            </div>

            <RolePreview role={role.id} />
          </div>
        </div>
      </div>
    </section>
  );
}
