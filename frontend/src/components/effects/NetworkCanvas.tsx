import React, { useEffect, useRef, useState } from "react";
import { useAudioEngine } from "./AudioEngine";

export type NetworkSimulationMode = "healthy" | "wire-drop" | "recovering" | "overdrive";

interface CanvasNode {
  id: string;
  label: string;
  role: string;
  nx: number;
  ny: number;
  color: string;
}

const NODES: CanvasNode[] = [
  { id: "bank-a", label: "Bank A", role: "Originator Hold", nx: 0.16, ny: 0.44, color: "#3b82f6" },
  { id: "switch", label: "TransactX Switch", role: "Saga Coordinator", nx: 0.5, ny: 0.34, color: "#10b981" },
  { id: "outbox", label: "PG Outbox · Merkle", role: "SHA-256 Digest", nx: 0.5, ny: 0.74, color: "#8b5cf6" },
  { id: "bank-b", label: "Bank B", role: "Credit Target", nx: 0.84, ny: 0.44, color: "#93b4ff" },
];

const EDGES: [number, number][] = [
  [0, 1],
  [1, 3],
  [1, 2],
  [2, 0],
  [2, 3],
];

export function NetworkCanvas({
  mode = "healthy",
  onNodeSelect,
  className = "",
  interactive = true,
  height = 280,
}: {
  mode?: NetworkSimulationMode;
  onNodeSelect?: (nodeId: string) => void;
  className?: string;
  interactive?: boolean;
  height?: number;
}) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const [selectedId, setSelectedId] = useState<string>("switch");
  const hoveredRef = useRef<number | null>(null);
  const pointerRef = useRef<{ x: number; y: number; active: boolean }>({ x: 0, y: 0, active: false });
  const ripplesRef = useRef<{ x: number; y: number; r: number; alpha: number; color: string }[]>([]);
  const trailRef = useRef<{ x: number; y: number; alpha: number }[]>([]);
  const { play } = useAudioEngine();

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const reduceMotion =
      typeof window !== "undefined" &&
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

    let visible = true;
    let tabVisible = typeof document !== "undefined" ? !document.hidden : true;
    let rafId = 0;

    // Ambient particle dots that react subtly to pointer proximity
    const ambientParticles = Array.from({ length: 28 }, (_, i) => ({
      bx: ((i * 73) % 100) / 100,
      by: ((i * 41) % 100) / 100,
      phase: i * 0.65,
      size: 1 + (i % 2) * 0.7,
    }));

    const packets = Array.from({ length: 9 }, (_, i) => ({
      edgeIdx: i % EDGES.length,
      t: (i / 9) % 1,
      speed: mode === "overdrive" ? 0.014 : 0.0055 + (i % 3) * 0.0014,
    }));

    const observer =
      typeof IntersectionObserver !== "undefined"
        ? new IntersectionObserver(
            (entries) => {
              visible = entries.some((entry) => entry.isIntersecting);
            },
            { threshold: 0.05 }
          )
        : null;
    observer?.observe(canvas);

    const onVisibility = () => {
      tabVisible = !document.hidden;
    };
    document.addEventListener("visibilitychange", onVisibility);

    const render = (now: number) => {
      if (visible && tabVisible) {
        const isDark = document.documentElement.dataset.theme === "dark";
        const rect = canvas.getBoundingClientRect();
        const width = Math.max(280, rect.width || 600);
        const h = Math.max(180, rect.height || height);
        const dpr = Math.min(2, window.devicePixelRatio || 1);

        if (canvas.width !== Math.round(width * dpr) || canvas.height !== Math.round(h * dpr)) {
          canvas.width = Math.round(width * dpr);
          canvas.height = Math.round(h * dpr);
        }
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        ctx.clearRect(0, 0, width, h);

        // 1. Subtle blueprint dot matrix illuminated around pointer
        const dotBase = isDark ? "rgba(148, 163, 184, 0.16)" : "rgba(15, 23, 42, 0.1)";
        const gridStep = 26;
        for (let gx = gridStep; gx < width; gx += gridStep) {
          for (let gy = gridStep; gy < h; gy += gridStep) {
            let alpha = isDark ? 0.16 : 0.1;
            if (pointerRef.current.active) {
              const dist = Math.hypot(gx - pointerRef.current.x, gy - pointerRef.current.y);
              if (dist < 110) alpha += (1 - dist / 110) * 0.35;
            }
            ctx.fillStyle = isDark
              ? `rgba(155, 185, 255, ${alpha.toFixed(2)})`
              : `rgba(29, 78, 216, ${alpha.toFixed(2)})`;
            ctx.fillRect(gx - 0.75, gy - 0.75, 1.5, 1.5);
          }
        }
        void dotBase;

        // 1b. Cursor trail particles (fading pointer vector trace)
        if (!reduceMotion && trailRef.current.length > 0) {
          for (let i = trailRef.current.length - 1; i >= 0; i--) {
            const pt = trailRef.current[i];
            pt.alpha -= 0.045;
            if (pt.alpha <= 0) {
              trailRef.current.splice(i, 1);
              continue;
            }
            ctx.beginPath();
            ctx.arc(pt.x, pt.y, 1.8 + pt.alpha * 2.2, 0, Math.PI * 2);
            ctx.fillStyle = isDark
              ? `rgba(155, 185, 255, ${(pt.alpha * 0.55).toFixed(2)})`
              : `rgba(29, 78, 216, ${(pt.alpha * 0.45).toFixed(2)})`;
            ctx.fill();
          }
        }

        // 2. Ambient cursor-reactive physics particles
        for (const p of ambientParticles) {
          let px = p.bx * width + Math.sin(now * 0.0007 + p.phase) * 9;
          let py = p.by * h + Math.cos(now * 0.0006 + p.phase) * 7;
          if (pointerRef.current.active && !reduceMotion) {
            const dx = px - pointerRef.current.x;
            const dy = py - pointerRef.current.y;
            const dist = Math.hypot(dx, dy);
            if (dist < 90 && dist > 1) {
              const push = ((90 - dist) / 90) * 18;
              px += (dx / dist) * push;
              py += (dy / dist) * push;
            }
          }
          ctx.fillStyle = isDark ? "rgba(155, 185, 255, 0.22)" : "rgba(29, 78, 216, 0.16)";
          ctx.beginPath();
          ctx.arc(px, py, p.size, 0, Math.PI * 2);
          ctx.fill();
        }

        // Compute pixel coords + hover check
        const nodePos = NODES.map((n) => ({
          ...n,
          x: n.nx * width,
          y: n.ny * h,
        }));

        if (pointerRef.current.active) {
          let found: number | null = null;
          nodePos.forEach((np, i) => {
            if (Math.hypot(np.x - pointerRef.current.x, np.y - pointerRef.current.y) < 34) {
              found = i;
            }
          });
          hoveredRef.current = found;
        }

        // 3. Edges + animated packet pulses
        EDGES.forEach(([aIdx, bIdx], edgeIdx) => {
          const a = nodePos[aIdx];
          const b = nodePos[bIdx];
          const midX = (a.x + b.x) / 2;
          const midY = (a.y + b.y) / 2 - (edgeIdx % 2 === 0 ? 18 : -16);
          const isFaultedEdge = mode === "wire-drop" && edgeIdx === 1;
          const isHighlighted =
            hoveredRef.current === aIdx ||
            hoveredRef.current === bIdx ||
            a.id === selectedId ||
            b.id === selectedId;

          ctx.save();
          ctx.beginPath();
          ctx.moveTo(a.x, a.y);
          ctx.quadraticCurveTo(midX, midY, b.x, b.y);
          ctx.lineWidth = isHighlighted ? 2 : 1.25;
          if (isFaultedEdge) {
            ctx.setLineDash([6, 5]);
            ctx.strokeStyle = isDark ? "rgba(248, 113, 113, 0.72)" : "rgba(220, 38, 38, 0.68)";
          } else {
            ctx.setLineDash([]);
            ctx.strokeStyle = isHighlighted
              ? isDark
                ? "rgba(147, 180, 255, 0.65)"
                : "rgba(37, 99, 235, 0.55)"
              : isDark
              ? "rgba(148, 163, 184, 0.25)"
              : "rgba(15, 23, 42, 0.18)";
          }
          ctx.stroke();
          ctx.restore();
        });

        // Packets on rails
        for (const pkt of packets) {
          if (!reduceMotion) {
            pkt.t = (pkt.t + (mode === "overdrive" ? 0.015 : pkt.speed)) % 1;
          }
          const [aIdx, bIdx] = EDGES[pkt.edgeIdx];
          const a = nodePos[aIdx];
          const b = nodePos[bIdx];
          const midX = (a.x + b.x) / 2;
          const midY = (a.y + b.y) / 2 - (pkt.edgeIdx % 2 === 0 ? 18 : -16);

          // In wire-drop mode, packets stop partway and blink crimson at wire gap
          const isFaultEdge = mode === "wire-drop" && pkt.edgeIdx === 1;
          const effectiveT = isFaultEdge ? Math.min(0.54, pkt.t) : pkt.t;
          const inv = 1 - effectiveT;
          const px = inv * inv * a.x + 2 * inv * effectiveT * midX + effectiveT * effectiveT * b.x;
          const py = inv * inv * a.y + 2 * inv * effectiveT * midY + effectiveT * effectiveT * b.y;

          const pColor = isFaultEdge
            ? "#ef4444"
            : mode === "recovering"
            ? "#f59e0b"
            : mode === "overdrive"
            ? "#10b981"
            : pkt.edgeIdx === 2 || pkt.edgeIdx === 3
            ? "#8b5cf6"
            : "#38bdf8";

          ctx.fillStyle = pColor;
          ctx.beginPath();
          ctx.arc(px, py, isFaultEdge && effectiveT >= 0.53 ? 4.2 : 3.2, 0, Math.PI * 2);
          ctx.fill();

          if (isFaultEdge && effectiveT >= 0.53) {
            ctx.strokeStyle = "rgba(239, 68, 68, 0.7)";
            ctx.lineWidth = 1.2;
            ctx.beginPath();
            ctx.moveTo(px - 6, py - 6);
            ctx.lineTo(px + 6, py + 6);
            ctx.moveTo(px + 6, py - 6);
            ctx.lineTo(px - 6, py + 6);
            ctx.stroke();
          }
        }

        // Click ripple rings
        ripplesRef.current = ripplesRef.current.filter((rp) => rp.alpha > 0.03);
        for (const rp of ripplesRef.current) {
          rp.r += 1.9;
          rp.alpha *= 0.94;
          ctx.strokeStyle = rp.color;
          ctx.globalAlpha = rp.alpha;
          ctx.lineWidth = 1.5;
          ctx.beginPath();
          ctx.arc(rp.x, rp.y, rp.r, 0, Math.PI * 2);
          ctx.stroke();
          ctx.globalAlpha = 1;
        }

        // 4. Service / Bank Nodes
        nodePos.forEach((node, idx) => {
          const isSelected = node.id === selectedId || hoveredRef.current === idx;
          const nodeStatusColor =
            mode === "wire-drop" && node.id === "bank-b"
              ? "#ef4444"
              : mode === "recovering" && node.id === "bank-b"
              ? "#f59e0b"
              : node.color;

          // Outer semantic status ring
          ctx.beginPath();
          ctx.arc(node.x, node.y, isSelected ? 22 : 18, 0, Math.PI * 2);
          ctx.fillStyle = isDark ? "#0f172a" : "#ffffff";
          ctx.fill();
          ctx.lineWidth = isSelected ? 2.5 : 1.75;
          ctx.strokeStyle = nodeStatusColor;
          ctx.stroke();

          // Inner status dot
          ctx.beginPath();
          ctx.arc(node.x, node.y, 4.5, 0, Math.PI * 2);
          ctx.fillStyle = nodeStatusColor;
          ctx.fill();

          // Label below node
          ctx.font = "600 11px 'JetBrains Mono', monospace";
          ctx.textAlign = "center";
          ctx.fillStyle = isDark ? "#f8fafc" : "#0f172a";
          ctx.fillText(node.label, node.x, node.y + 36);

          ctx.font = "400 9.5px 'JetBrains Mono', monospace";
          ctx.fillStyle = isDark ? "#94a3b8" : "#475569";
          ctx.fillText(
            mode === "wire-drop" && node.id === "bank-b" ? "Reply Timeout" : node.role,
            node.x,
            node.y + 49
          );
        });
      }

      if (!reduceMotion) {
        rafId = requestAnimationFrame(render);
      }
    };

    rafId = requestAnimationFrame(render);
    return () => {
      cancelAnimationFrame(rafId);
      observer?.disconnect();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [height, mode, selectedId]);

  const handlePointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!interactive) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    pointerRef.current = { x, y, active: true };
    trailRef.current.push({ x, y, alpha: 0.9 });
    if (trailRef.current.length > 14) {
      trailRef.current.shift();
    }
  };

  const handleClick = (e: React.MouseEvent<HTMLCanvasElement>) => {
    if (!interactive) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    let clickedNode: CanvasNode | null = null;
    for (const n of NODES) {
      if (Math.hypot(n.nx * rect.width - x, n.ny * rect.height - y) < 38) {
        clickedNode = n;
        break;
      }
    }
    if (clickedNode) {
      setSelectedId(clickedNode.id);
      onNodeSelect?.(clickedNode.id);
      play("packet");
      ripplesRef.current.push({ x, y, r: 10, alpha: 0.9, color: clickedNode.color });
    } else {
      play("tick");
      ripplesRef.current.push({ x, y, r: 8, alpha: 0.7, color: "#3b82f6" });
    }
  };

  return (
    <canvas
      ref={canvasRef}
      data-cursor={interactive ? "Probe Node" : undefined}
      onPointerMove={handlePointerMove}
      onPointerLeave={() => {
        pointerRef.current.active = false;
        hoveredRef.current = null;
      }}
      onClick={handleClick}
      style={{ height }}
      className={`w-full block rounded-xl cursor-crosshair ${className}`}
      aria-label="Interactive two-bank simulated payment network topology canvas"
    />
  );
}
