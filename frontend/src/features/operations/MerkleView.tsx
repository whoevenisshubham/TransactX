import React, { useRef, useState } from "react";
import { toast } from "sonner";
import { api, ApiError } from "../../api";
import { Button, InlineError, PageHeader } from "../../components";
import { DotSentinel, useAudioEngine } from "../../components/effects";
import type { MerkleTreeChild, MerkleTreeRoot } from "./types";

export function MerkleView({ token }: { token: string }) {
  const [participantId, setParticipantId] = useState("");
  const [scopeFrom, setScopeFrom] = useState("");
  const [scopeTo, setScopeTo] = useState("");

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [root, setRoot] = useState<MerkleTreeRoot | null>(null);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const { play } = useAudioEngine();
  const [rootContext, setRootContext] = useState<{
    participantId: string;
    scopeFrom: string;
    scopeTo: string;
  } | null>(null);
  const treeGeneration = useRef(0);
  const [childrenMap, setChildrenMap] = useState<Record<string, MerkleTreeChild[]>>({});

  async function fetchRoot(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    const requestGeneration = ++treeGeneration.current;
    setRoot(null);
    setRootContext(null);
    setChildrenMap({});
    try {
      const res = await api.opsReconciliationTreeRoot(participantId, scopeFrom, scopeTo, token);
      if (requestGeneration === treeGeneration.current) {
        setRoot(res);
        setRootContext({ participantId, scopeFrom, scopeTo });
      }
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Failed to fetch Merkle root");
    } finally {
      setLoading(false);
    }
  }

  async function fetchChildren(generation: string, path: string) {
    const key = `${generation}:${path}`;
    if (childrenMap[key] || !rootContext) return;
    const requestGeneration = treeGeneration.current;
    try {
      const res = await api.opsReconciliationTreeChildren(
        rootContext.participantId,
        rootContext.scopeFrom,
        rootContext.scopeTo,
        generation,
        path,
        token
      );
      if (requestGeneration === treeGeneration.current) {
        setChildrenMap((prev) => ({ ...prev, [key]: res }));
        setError("");
      }
    } catch (err) {
      if (requestGeneration === treeGeneration.current) {
        setError(err instanceof Error ? err.message : "Child proof evidence is unavailable.");
      }
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
            <label htmlFor="operator-participant-id-4">Participant ID</label>
            <input
              id="operator-participant-id-4"
              value={participantId}
              onChange={(e) => setParticipantId(e.target.value)}
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="operator-scope-from-iso8601-5">Scope From (ISO8601)</label>
            <input
              id="operator-scope-from-iso8601-5"
              value={scopeFrom}
              onChange={(e) => setScopeFrom(e.target.value)}
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="operator-scope-to-iso8601-6">Scope To (ISO8601)</label>
            <input
              id="operator-scope-to-iso8601-6"
              value={scopeTo}
              onChange={(e) => setScopeTo(e.target.value)}
              required
            />
          </div>
          <div className="form-actions">
            <Button type="submit" disabled={loading}>
              Fetch Root
            </Button>
          </div>
        </form>
        {error && <InlineError message={error} />}
      </div>

      {!root && !loading && !error && (
        <div className="console-card space-y-3">
          <h3>How Merkle bucket verification works</h3>
          <p className="ops-muted m-0">
            Enter a participant ID and UTC time scope above to inspect its committed root hash and walk bucket partition children.
          </p>
          <div className="console-spec-list">
            <div className="console-spec-item">
              <span className="console-spec-label">Accumulator specification</span>
              <span className="console-spec-val">merkle-v1 · SHA-256</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Domain separation</span>
              <span className="console-spec-val">TXNODE|v1|&lt;left&gt;|&lt;right&gt;</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Scope format</span>
              <span className="console-spec-val">ISO8601 UTC (e.g. 2026-10-18T00:00:00Z)</span>
            </div>
          </div>
        </div>
      )}

      {root && (
        <div className="console-card">
          <h3>Merkle Root</h3>
          <div className="console-spec-list">
            <div className="console-spec-item">
              <span className="console-spec-label">Root Hash</span>
              <span className="console-spec-val" style={{ fontFamily: "monospace", wordBreak: "break-all" }}>
                {root.rootHex}
              </span>
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
              <span className="console-spec-val">
                {root.region.Start} - {root.region.End}
              </span>
            </div>
          </div>
          <div style={{ marginTop: "1.5rem" }}>
            <Button variant="secondary" onClick={() => fetchChildren(root.ref.Generation, root.ref.Path)}>
              Load Children
            </Button>
          </div>

          {childrenMap[`${root.ref.Generation}:${root.ref.Path}`] && (
            <div style={{ marginTop: "1.25rem" }} className="space-y-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h4 className="inline-flex items-center gap-2 m-0">
                  <DotSentinel state="success" />
                  Tree nodes
                </h4>
                {selectedPath && (
                  <span className="text-xs font-mono px-2.5 py-0.5 rounded-full bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)] text-[var(--accent-strong)]">
                    Focused path: {selectedPath}
                  </span>
                )}
              </div>
              {childrenMap[`${root.ref.Generation}:${root.ref.Path}`].map((c, i) => (
                <MerkleNodeBranch
                  key={`${c.ref.Generation}:${c.ref.Path}:${i}`}
                  node={c}
                  depth={1}
                  childrenMap={childrenMap}
                  selectedPath={selectedPath}
                  onSelectPath={(path) => {
                    setSelectedPath(path);
                    play("tab");
                  }}
                  onCopyHash={(hex) => {
                    navigator.clipboard.writeText(hex).then(
                      () => {
                        play("tick");
                        toast.success("Merkle node hash copied");
                      },
                      () => toast.error("Clipboard copy unavailable")
                    );
                  }}
                  fetchChildren={fetchChildren}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </>
  );
}

function MerkleNodeBranch({
  node,
  depth,
  childrenMap,
  selectedPath,
  onSelectPath,
  onCopyHash,
  fetchChildren,
}: {
  node: MerkleTreeChild;
  depth: number;
  childrenMap: Record<string, MerkleTreeChild[]>;
  selectedPath: string | null;
  onSelectPath: (path: string) => void;
  onCopyHash: (hex: string) => void;
  fetchChildren: (generation: string, path: string) => Promise<void>;
}) {
  const key = `${node.ref.Generation}:${node.ref.Path}`;
  const children = childrenMap[key];
  const isSelected = selectedPath === node.ref.Path;
  const isAncestor = selectedPath !== null && selectedPath.startsWith(`${node.ref.Path}/`);

  return (
      <div
        className={`console-spec-list rounded-xl border transition-colors ${
          isSelected
            ? "border-[var(--action-accent-bg)] ring-1 ring-[var(--action-accent-bg)]/30"
            : isAncestor
            ? "border-[var(--border-active)]"
            : "border-[var(--border-hairline)]"
        }`}
        onClick={(e) => {
          e.stopPropagation();
          onSelectPath(node.ref.Path);
        }}
        style={{
          background: "var(--bg-surface-raised)",
          padding: "0.75rem 1rem",
          margin: "0.625rem 0 0.625rem 1rem",
          borderLeft: `3px solid ${isSelected ? "var(--blueprint-emerald)" : "var(--action-accent-bg)"}`,
        }}
      >
      <div className="console-spec-item">
        <span className="console-spec-label">Depth / path</span>
        <span className="console-spec-val mono">
          {depth} · {node.ref.Path}
        </span>
      </div>
      <div className="console-spec-item">
        <span className="console-spec-label">Parent</span>
        <span className="console-spec-val mono">
          {depth === 1 ? "root" : node.ref.Path.split("/").slice(0, -1).join("/") || "root"}
        </span>
      </div>
      <div className="console-spec-item">
        <span className="console-spec-label">Hash</span>
        <span className="console-spec-val mono inline-flex items-center gap-2 flex-wrap" style={{ fontSize: "0.85rem", wordBreak: "break-all" }}>
          <span>{node.hashHex}</span>
          <button
            type="button"
            className="text-[10px] font-mono px-2 py-0.5 rounded border border-[var(--border-hairline)] bg-[var(--bg-surface)] text-[var(--text-secondary)] hover:text-[var(--text-primary)] cursor-pointer"
            onClick={(e) => {
              e.stopPropagation();
              onCopyHash(node.hashHex);
            }}
          >
            Copy hash
          </button>
        </span>
      </div>
      <div className="console-spec-item">
        <span className="console-spec-label">Region</span>
        <span className="console-spec-val">
          {node.region.Start} – {node.region.End}
        </span>
      </div>
      {children === undefined && (
        <Button variant="quiet" onClick={() => fetchChildren(node.ref.Generation, node.ref.Path)}>
          Expand node
        </Button>
      )}
      {children?.length === 0 && <span className="console-meta-text">Leaf bucket</span>}
      {children?.map((child, index) => (
        <MerkleNodeBranch
          key={`${child.ref.Generation}:${child.ref.Path}:${index}`}
          node={child}
          depth={depth + 1}
          childrenMap={childrenMap}
          selectedPath={selectedPath}
          onSelectPath={onSelectPath}
          onCopyHash={onCopyHash}
          fetchChildren={fetchChildren}
        />
      ))}
    </div>
  );
}
