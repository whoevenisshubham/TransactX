import React, { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Maximize2, ZoomIn, ZoomOut, RotateCcw, Play, Pause, X, GitBranch } from "lucide-react";
import { useAudioEngine } from "../../components/effects";

export interface BlueprintTab {
  id: string;
  label: string;
  subtitle: string;
  componentsCount: number;
  edgesCount: number;
  takeaways: string[];
  nodes: {
    id: string;
    label: string;
    sub: string;
    x: number;
    y: number;
    w: number;
    h: number;
    color: string;
    role: string;
  }[];
  edges: {
    from: string;
    to: string;
    label: string;
    color: string;
  }[];
}

export const BLUEPRINTS: BlueprintTab[] = [
  {
    id: "overview",
    label: "01. System Overview",
    subtitle: "Isolated participant schemas coordinated over HTTP + durable leases",
    componentsCount: 6,
    edgesCount: 7,
    takeaways: [
      "Client UUIDv4 reservation prevents duplicate money transfers across retries and offline reconnects.",
      "Central Switch never holds distributed database row locks across Bank A or Bank B schemas.",
      "Transactional outbox drives worklist projections, SSE live updates, and SHA-256 Merkle windows.",
    ],
    nodes: [
      { id: "client", label: "Client / Offline IDB", sub: "UUIDv4 Intent", x: 30, y: 120, w: 145, h: 64, color: "#93b4ff", role: "Originates payment with idempotency key; zero automatic financial retries." },
      { id: "api", label: "Go Switch API", sub: "Validation · RBAC", x: 245, y: 120, w: 155, h: 64, color: "#3b82f6", role: "Persists intent atomically before dispatching two-phase bank operations." },
      { id: "bank-a", label: "Bank A Schema", sub: "bank_a Escrow", x: 485, y: 34, w: 155, h: 64, color: "#10b981", role: "Acquires isolated debit hold with bounded lease window." },
      { id: "bank-b", label: "Bank B Schema", sub: "bank_b Credit", x: 485, y: 206, w: 155, h: 64, color: "#10b981", role: "Executes credit target leg behind isolated health & circuit breakers." },
      { id: "pg", label: "PG Outbox + Worker", sub: "LeaseEpoch Fencing", x: 245, y: 245, w: 155, h: 64, color: "#8b5cf6", role: "Background recovery claims PENDING_RECONCILIATION safely via HeartbeatLease." },
      { id: "merkle", label: "Merkle · Ops Console", sub: "Audit & Evidence", x: 705, y: 120, w: 155, h: 64, color: "#f59e0b", role: "Commits bucket digests (merkle-v1) and streams forensic timelines." },
    ],
    edges: [
      { from: "client", to: "api", label: "Idempotent POST", color: "#3b82f6" },
      { from: "api", to: "bank-a", label: "1. Debit Hold", color: "#10b981" },
      { from: "api", to: "bank-b", label: "2. Credit Target", color: "#3b82f6" },
      { from: "api", to: "pg", label: "Outbox Flush", color: "#8b5cf6" },
      { from: "pg", to: "bank-b", label: "Probe on Drop", color: "#f59e0b" },
      { from: "bank-a", to: "merkle", label: "SQL SUM Proof", color: "#10b981" },
      { from: "bank-b", to: "merkle", label: "Bucket Digest", color: "#f59e0b" },
    ],
  },
  {
    id: "recovery",
    label: "02. Recovery & Leases",
    subtitle: "Monotonic LeaseEpoch fencing resolves lost replies without duplicate debits",
    componentsCount: 5,
    edgesCount: 5,
    takeaways: [
      "Timeout or dropped 200 OK transitions payment into PENDING_RECONCILIATION — never presumed failed.",
      "HeartbeatLease prevents split-brain recovery workers from racing the same transfer.",
      "Queries target the original operation reference before compensating or completing.",
    ],
    nodes: [
      { id: "timeout", label: "Wire Timeout / 504", sub: "Unknown Outcome", x: 35, y: 120, w: 155, h: 64, color: "#ef4444", role: "Dropped reply leaves central switch uncertain while Bank B may have settled." },
      { id: "reconcile", label: "PENDING_RECON", sub: "Safe Quarantine", x: 255, y: 120, w: 160, h: 64, color: "#f59e0b", role: "Suspends finality until participant status can be proved." },
      { id: "lease", label: "Worker LeaseEpoch", sub: "Fenced Claim", x: 475, y: 40, w: 155, h: 64, color: "#8b5cf6", role: "Only one lease holder probes the original idempotent transfer." },
      { id: "probe", label: "Bank Status Probe", sub: "Lookup Original ID", x: 475, y: 210, w: 155, h: 64, color: "#3b82f6", role: "Retrieves authoritative bank observation rather than retrying a debit." },
      { id: "settled", label: "Converged Finality", sub: "Completed / Reversed", x: 700, y: 120, w: 155, h: 64, color: "#10b981", role: "Commits evidence trail and cites timestamps inside Payment Journey." },
    ],
    edges: [
      { from: "timeout", to: "reconcile", label: "Never Assume Fail", color: "#ef4444" },
      { from: "reconcile", to: "lease", label: "ClaimDueJobs", color: "#8b5cf6" },
      { from: "lease", to: "probe", label: "Fenced Probe", color: "#3b82f6" },
      { from: "probe", to: "settled", label: "True Evidence", color: "#10b981" },
      { from: "reconcile", to: "settled", label: "Outbox Emit", color: "#f59e0b" },
    ],
  },
  {
    id: "merkle",
    label: "03. Merkle & Conservation",
    subtitle: "Double-entry SQL equilibrium paired with SHA-256 TXNODE|v1| trees",
    componentsCount: 4,
    edgesCount: 4,
    takeaways: [
      "Every monetary amount stays int64 / BigInt paise from UI validation to SQL settlement rows.",
      "SQL conservation checks verify SUM(debit_paise - credit_paise) = 0 per participant.",
      "Inspecting discrepancies walks O(log N) Merkle buckets rather than brute-forcing ledger tables.",
    ],
    nodes: [
      { id: "entries", label: "Ledger Postings", sub: "Integer Paise", x: 45, y: 120, w: 155, h: 64, color: "#3b82f6", role: "Immutable double-entry postings stored per sovereign participant." },
      { id: "sqlsum", label: "SQL Conservation", sub: "DEBIT == CREDIT", x: 265, y: 42, w: 160, h: 64, color: "#10b981", role: "Global balance invariant invariant scan verifies zero drift." },
      { id: "buckets", label: "Merkle Bucketizer", sub: "SHA256 TXNODE|v1|", x: 265, y: 204, w: 160, h: 64, color: "#8b5cf6", role: "Deterministic time-bucket binary hash commitments." },
      { id: "proof", label: "Inclusion Verifier", sub: "O(log N) Proof", x: 540, y: 120, w: 165, h: 64, color: "#f59e0b", role: "Reconstructs root digest from leaf hash + sibling inclusion path." },
    ],
    edges: [
      { from: "entries", to: "sqlsum", label: "Audit SUM", color: "#10b981" },
      { from: "entries", to: "buckets", label: "Hash Leaves", color: "#8b5cf6" },
      { from: "buckets", to: "proof", label: "Root Commit", color: "#f59e0b" },
      { from: "sqlsum", to: "proof", label: "Verified Frame", color: "#10b981" },
    ],
  },
];

function BlueprintCanvasSvg({
  blueprint,
  live,
  hoveredNode,
  selectedNode,
  onSelectNode,
  onHoverNode,
  zoom = 1,
  pan = { x: 0, y: 0 },
}: {
  blueprint: BlueprintTab;
  live: boolean;
  hoveredNode: string | null;
  selectedNode: string | null;
  onSelectNode: (id: string) => void;
  onHoverNode: (id: string | null) => void;
  zoom?: number;
  pan?: { x: number; y: number };
}) {
  const focusId = hoveredNode ?? selectedNode;
  const nodeMap = new Map(blueprint.nodes.map((n) => [n.id, n]));

  return (
    <svg
      viewBox="0 0 890 330"
      className="w-full h-full select-none"
      role="img"
      aria-label={`${blueprint.label} interactive architecture diagram`}
    >
      <defs>
        <pattern id="bp-dots" width="24" height="24" patternUnits="userSpaceOnUse">
          <circle cx="1.5" cy="1.5" r="1.2" fill="var(--blueprint-grid)" />
        </pattern>
      </defs>

      <rect width="890" height="330" fill="var(--blueprint-bg)" rx="12" />
      <rect width="890" height="330" fill="url(#bp-dots)" rx="12" />

      <g transform={`translate(${pan.x}, ${pan.y}) scale(${zoom})`} style={{ transformOrigin: "445px 165px" }}>
        {/* Edges */}
        {blueprint.edges.map((edge, idx) => {
          const from = nodeMap.get(edge.from);
          const to = nodeMap.get(edge.to);
          if (!from || !to) return null;
          const x1 = from.x + from.w / 2;
          const y1 = from.y + from.h / 2;
          const x2 = to.x + to.w / 2;
          const y2 = to.y + to.h / 2;
          const mx = (x1 + x2) / 2;
          const my = (y1 + y2) / 2 - (idx % 2 === 0 ? 18 : -18);
          const connected = !focusId || edge.from === focusId || edge.to === focusId;

          return (
            <g key={`${edge.from}-${edge.to}`} style={{ opacity: connected ? 1 : 0.25, transition: "opacity 180ms" }}>
              <path
                d={`M ${x1} ${y1} Q ${mx} ${my} ${x2} ${y2}`}
                fill="none"
                stroke={edge.color}
                strokeWidth={connected && focusId ? 2.6 : 1.75}
                strokeDasharray={live ? "6 5" : undefined}
              >
                {live && (
                  <animate
                    attributeName="stroke-dashoffset"
                    from="22"
                    to="0"
                    dur={`${1.1 + idx * 0.15}s`}
                    repeatCount="indefinite"
                  />
                )}
              </path>
              <rect
                x={mx - 42}
                y={my - 9}
                width="84"
                height="16"
                rx="4"
                fill="var(--bg-surface)"
                stroke="var(--border-hairline)"
                strokeWidth="0.8"
              />
              <text
                x={mx}
                y={my + 2}
                textAnchor="middle"
                className="font-mono"
                fontSize="8.5"
                fill="var(--text-secondary)"
              >
                {edge.label}
              </text>
            </g>
          );
        })}

        {/* Nodes */}
        {blueprint.nodes.map((node) => {
          const active = node.id === selectedNode || node.id === hoveredNode;
          const dimmed = focusId !== null && !active && !blueprint.edges.some(
            (e) => (e.from === focusId && e.to === node.id) || (e.to === focusId && e.from === node.id)
          );

          return (
            <g
              key={node.id}
              transform={`translate(${node.x}, ${node.y})`}
              onClick={() => onSelectNode(node.id)}
              onMouseEnter={() => onHoverNode(node.id)}
              onMouseLeave={() => onHoverNode(null)}
              className="cursor-pointer"
              style={{ opacity: dimmed ? 0.35 : 1, transition: "opacity 180ms" }}
            >
              <rect
                width={node.w}
                height={node.h}
                rx="10"
                fill="var(--bg-surface)"
                stroke={active ? node.color : "var(--border-active)"}
                strokeWidth={active ? 2.4 : 1.25}
              />
              <circle cx="14" cy="20" r="4" fill={node.color} />
              <text
                x="25"
                y="24"
                className="font-mono"
                fontSize="11.5"
                fontWeight="700"
                fill="var(--text-primary)"
              >
                {node.label}
              </text>
              <text
                x="14"
                y="45"
                className="font-mono"
                fontSize="9.5"
                fill="var(--text-tertiary)"
              >
                {node.sub}
              </text>
            </g>
          );
        })}
      </g>
    </svg>
  );
}

export function BlueprintExplorerSection() {
  const [tabIdx, setTabIdx] = useState(0);
  const [live, setLive] = useState(true);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [hoveredNodeId, setHoveredNodeId] = useState<string | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const { play } = useAudioEngine();
  const expandTriggerRef = useRef<HTMLButtonElement | null>(null);
  const modalContainerRef = useRef<HTMLDivElement | null>(null);
  const wasOpenRef = useRef(false);

  useEffect(() => {
    if (modalOpen) {
      wasOpenRef.current = true;
      const focusableSelector =
        'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

      const rafId = window.requestAnimationFrame(() => {
        const container = modalContainerRef.current;
        if (!container) return;
        const firstFocusable = container.querySelector<HTMLElement>(focusableSelector);
        if (firstFocusable) {
          firstFocusable.focus();
        } else {
          container.focus();
        }
      });

      const onKeyDown = (e: KeyboardEvent) => {
        if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          setModalOpen(false);
          return;
        }
        if (e.key === "Tab") {
          const container = modalContainerRef.current;
          if (!container) return;
          const focusables = Array.from(
            container.querySelectorAll<HTMLElement>(focusableSelector)
          ).filter((el) => el.getClientRects().length > 0);
          if (focusables.length === 0) {
            e.preventDefault();
            container.focus();
            return;
          }
          const first = focusables[0];
          const last = focusables[focusables.length - 1];
          const active = document.activeElement as HTMLElement | null;
          if (!active || !container.contains(active)) {
            e.preventDefault();
            (e.shiftKey ? last : first).focus();
          } else if (e.shiftKey && active === first) {
            e.preventDefault();
            last.focus();
          } else if (!e.shiftKey && active === last) {
            e.preventDefault();
            first.focus();
          }
        }
      };

      document.addEventListener("keydown", onKeyDown, true);
      return () => {
        window.cancelAnimationFrame(rafId);
        document.removeEventListener("keydown", onKeyDown, true);
      };
    }

    if (wasOpenRef.current) {
      wasOpenRef.current = false;
      expandTriggerRef.current?.focus();
    }
  }, [modalOpen]);

  const blueprint = BLUEPRINTS[tabIdx];
  const inspectedId = hoveredNodeId ?? selectedNodeId ?? blueprint.nodes[0].id;
  const inspectedNode = blueprint.nodes.find((n) => n.id === inspectedId) ?? blueprint.nodes[0];

  return (
    <div className="rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-5 sm:p-6 shadow-[var(--shadow-raised)] space-y-5">
      {/* Header + Mode Switcher */}
      <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4 border-b border-[var(--border-hairline)] pb-4">
        <div className="space-y-1">
          <div className="inline-flex items-center gap-2 text-xs font-mono font-semibold text-[var(--blueprint-cobalt,#2563eb)]">
            <GitBranch className="w-3.5 h-3.5" aria-hidden="true" />
            <span>Interactive Architecture Blueprint</span>
          </div>
          <h3 className="font-display text-xl font-bold tracking-tight text-[var(--text-primary)] m-0">
            {blueprint.label} — {blueprint.componentsCount} components · {blueprint.edgesCount} edges
          </h3>
          <p className="text-xs text-[var(--text-secondary)] m-0">{blueprint.subtitle}</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="inline-flex rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] p-0.5">
            {BLUEPRINTS.map((bp, idx) => (
              <button
                key={bp.id}
                type="button"
                onClick={() => {
                  play("tab");
                  setTabIdx(idx);
                  setSelectedNodeId(null);
                  setHoveredNodeId(null);
                }}
                className={`rounded-md px-2.5 py-1 text-[11px] font-mono font-semibold transition-colors cursor-pointer ${
                  tabIdx === idx
                    ? "bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[var(--shadow-subtle)]"
                    : "text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                }`}
              >
                {bp.label.split(".")[0]}
              </button>
            ))}
          </div>

          <button
            type="button"
            onClick={() => {
              play("tick");
              setLive((v) => !v);
            }}
            className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-2.5 py-1.5 text-[11px] font-mono font-medium text-[var(--text-primary)] cursor-pointer hover:border-[var(--border-active)]"
          >
            {live ? <Pause className="w-3 h-3 text-[var(--blueprint-emerald,#10b981)]" /> : <Play className="w-3 h-3" />}
            <span>{live ? "Live Packets" : "Static"}</span>
          </button>

          <button
            ref={expandTriggerRef}
            type="button"
            onClick={() => {
              play("modal");
              setZoom(1);
              setPan({ x: 0, y: 0 });
              setModalOpen(true);
            }}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[var(--action-primary-bg)] px-3 py-1.5 text-[11px] font-mono font-semibold !text-[var(--action-primary-fg)] cursor-pointer hover:opacity-90"
          >
            <Maximize2 className="w-3 h-3" aria-hidden="true" />
            <span>Expand blueprint</span>
          </button>
        </div>
      </div>

      {/* Inline Interactive Canvas */}
      <div
        data-cursor="Inspect Node"
        className="rounded-xl border border-[var(--border-hairline)] overflow-hidden aspect-[890/330] max-h-[340px]"
      >
        <BlueprintCanvasSvg
          blueprint={blueprint}
          live={live}
          hoveredNode={hoveredNodeId}
          selectedNode={selectedNodeId}
          onSelectNode={(id) => {
            play("packet");
            setSelectedNodeId(id);
          }}
          onHoverNode={setHoveredNodeId}
        />
      </div>

      {/* Node Inspector Strip + Takeaways */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-4 items-center">
        <div className="lg:col-span-5 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 p-3.5 text-xs space-y-1">
          <div className="flex items-center justify-between font-mono">
            <span className="font-bold text-[var(--text-primary)]">{inspectedNode.label}</span>
            <span className="text-[10px] px-2 py-0.5 rounded bg-[var(--bg-surface)] border border-[var(--border-hairline)] text-[var(--text-tertiary)]">
              {inspectedNode.sub}
            </span>
          </div>
          <p className="text-[var(--text-secondary)] leading-relaxed m-0">{inspectedNode.role}</p>
        </div>

        <ul className="lg:col-span-7 grid grid-cols-1 sm:grid-cols-3 gap-2.5 text-[11px] text-[var(--text-secondary)] m-0 p-0 list-none">
          {blueprint.takeaways.map((line) => (
            <li
              key={line}
              className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-canvas)]/70 px-3 py-2 leading-relaxed"
            >
              {line}
            </li>
          ))}
        </ul>
      </div>

      {/* Expanded Blueprint Pan/Zoom Modal (portaled to body to escape clip-path/transform stacking contexts) */}
      {modalOpen &&
        typeof document !== "undefined" &&
        createPortal(
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Expanded system architecture blueprint explorer"
            className="fixed inset-0 z-50 flex flex-col bg-[#060911]/85 p-4 sm:p-6 backdrop-blur-md"
            onClick={() => setModalOpen(false)}
          >
            <div
              ref={modalContainerRef}
              tabIndex={-1}
              className="mx-auto flex w-full max-w-6xl flex-1 flex-col rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-[var(--shadow-modal)] overflow-hidden focus:outline-none"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--border-hairline)] px-4 py-3">
                <div className="flex items-center gap-3">
                  <span className="font-display text-sm font-bold text-[var(--text-primary)]">
                    {blueprint.label}
                  </span>
                  <span className="font-mono text-xs text-[var(--text-tertiary)]">
                    {Math.round(zoom * 100)}% zoom · Click any node to trace edges
                  </span>
                </div>
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => setZoom((z) => Math.max(0.75, +(z - 0.15).toFixed(2)))}
                    aria-label="Zoom out diagram"
                    className="rounded-lg border border-[var(--border-hairline)] p-1.5 text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] cursor-pointer"
                  >
                    <ZoomOut className="w-4 h-4" />
                  </button>
                  <button
                    type="button"
                    onClick={() => setZoom((z) => Math.min(1.65, +(z + 0.15).toFixed(2)))}
                    aria-label="Zoom in diagram"
                    className="rounded-lg border border-[var(--border-hairline)] p-1.5 text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] cursor-pointer"
                  >
                    <ZoomIn className="w-4 h-4" />
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setZoom(1);
                      setPan({ x: 0, y: 0 });
                      setSelectedNodeId(null);
                    }}
                    aria-label="Reset diagram view"
                    className="rounded-lg border border-[var(--border-hairline)] p-1.5 text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] cursor-pointer"
                  >
                    <RotateCcw className="w-4 h-4" />
                  </button>
                  <button
                    type="button"
                    onClick={() => setModalOpen(false)}
                    aria-label="Close blueprint modal"
                    className="rounded-lg border border-[var(--border-hairline)] p-1.5 text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] cursor-pointer ml-1"
                  >
                    <X className="w-4 h-4" />
                  </button>
                </div>
              </div>

              <div className="flex-1 min-h-0 p-4 flex flex-col justify-center overflow-hidden">
                <BlueprintCanvasSvg
                  blueprint={blueprint}
                  live={live}
                  hoveredNode={hoveredNodeId}
                  selectedNode={selectedNodeId}
                  onSelectNode={(id) => {
                    play("packet");
                    setSelectedNodeId(id);
                  }}
                  onHoverNode={setHoveredNodeId}
                  zoom={zoom}
                  pan={pan}
                />
              </div>

              <div className="border-t border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 px-4 py-3 flex flex-wrap items-center justify-between gap-2 text-xs font-mono">
                <span>
                  <strong className="text-[var(--text-primary)]">{inspectedNode.label}:</strong>{" "}
                  <span className="text-[var(--text-secondary)]">{inspectedNode.role}</span>
                </span>
                <div className="flex gap-1.5">
                  <button type="button" onClick={() => setPan((p) => ({ ...p, x: p.x - 30 }))} className="px-2 py-0.5 rounded border border-[var(--border-hairline)] bg-[var(--bg-surface)] cursor-pointer">←</button>
                  <button type="button" onClick={() => setPan((p) => ({ ...p, x: p.x + 30 }))} className="px-2 py-0.5 rounded border border-[var(--border-hairline)] bg-[var(--bg-surface)] cursor-pointer">→</button>
                </div>
              </div>
            </div>
          </div>,
          document.body
        )}
    </div>
  );
}
