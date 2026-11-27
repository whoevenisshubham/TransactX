import React, { useRef, useState } from "react";
import { useReducedMotion } from "motion/react";

export function SpotlightCard({
  children,
  className = "",
  tilt = true,
  accent = "rgba(59, 130, 246, 0.16)",
  as = "div",
  id,
  "data-cursor": dataCursor,
}: {
  children: React.ReactNode;
  className?: string;
  tilt?: boolean;
  accent?: string;
  as?: "div" | "section" | "article";
  id?: string;
  "data-cursor"?: string;
}) {
  const reduce = useReducedMotion();
  const cardRef = useRef<HTMLElement | null>(null);
  const [coords, setCoords] = useState<{ x: number; y: number; rx: number; ry: number; active: boolean }>({
    x: 50,
    y: 50,
    rx: 0,
    ry: 0,
    active: false,
  });

  const handleMove = (e: React.PointerEvent<HTMLElement>) => {
    if (reduce || e.pointerType === "touch") return;
    const el = cardRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    const px = ((e.clientX - rect.left) / rect.width) * 100;
    const py = ((e.clientY - rect.top) / rect.height) * 100;
    const rx = tilt ? ((50 - py) / 50) * 3.2 : 0;
    const ry = tilt ? ((px - 50) / 50) * 3.2 : 0;
    setCoords({ x: px, y: py, rx, ry, active: true });
  };

  const handleLeave = () => {
    setCoords((prev) => ({ ...prev, rx: 0, ry: 0, active: false }));
  };

  const Tag = as;

  return (
    <Tag
      ref={(node: HTMLElement | null) => {
        cardRef.current = node;
      }}
      id={id}
      data-cursor={dataCursor}
      onPointerMove={handleMove}
      onPointerLeave={handleLeave}
      style={{
        transform:
          !reduce && coords.active && tilt
            ? `perspective(950px) rotateX(${coords.rx.toFixed(2)}deg) rotateY(${coords.ry.toFixed(2)}deg)`
            : undefined,
      }}
      className={`group/spot relative overflow-hidden transition-[transform,border-color,box-shadow] duration-200 ease-out will-change-transform ${className}`}
    >
      {/* Dynamic cursor spotlight overlay */}
      <span
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 transition-opacity duration-300"
        style={{
          opacity: coords.active ? 1 : 0,
          background: `radial-gradient(360px circle at ${coords.x}% ${coords.y}%, ${accent}, transparent 70%)`,
        }}
      />
      {children}
    </Tag>
  );
}
