import React, { useRef, useState } from "react";
import { api, ApiError } from "../../api";
import { Button, InlineError, PageHeader } from "../../components";
import type { MerkleTreeChild, MerkleTreeRoot } from "./types";

export function MerkleView({ token }: { token: string }) {
  const [participantId, setParticipantId] = useState("");
  const [scopeFrom, setScopeFrom] = useState("");
  const [scopeTo, setScopeTo] = useState("");

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [root, setRoot] = useState<MerkleTreeRoot | null>(null);
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
            <div style={{ marginTop: "1rem" }}>
              <h4>Tree nodes</h4>
              {childrenMap[`${root.ref.Generation}:${root.ref.Path}`].map((c, i) => (
                <MerkleNodeBranch
                  key={`${c.ref.Generation}:${c.ref.Path}:${i}`}
                  node={c}
                  depth={1}
                  childrenMap={childrenMap}
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
  fetchChildren,
}: {
  node: MerkleTreeChild;
  depth: number;
  childrenMap: Record<string, MerkleTreeChild[]>;
  fetchChildren: (generation: string, path: string) => Promise<void>;
}) {
  const key = `${node.ref.Generation}:${node.ref.Path}`;
  const children = childrenMap[key];
  return (
    <div
      className="console-spec-list"
      style={{
        background: "var(--bg-subtle)",
        padding: "0.65rem",
        margin: "0.5rem 0 0.5rem 1rem",
        borderLeft: "2px solid var(--border-color)",
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
        <span className="console-spec-val mono" style={{ fontSize: "0.85rem", wordBreak: "break-all" }}>
          {node.hashHex}
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
          fetchChildren={fetchChildren}
        />
      ))}
    </div>
  );
}
