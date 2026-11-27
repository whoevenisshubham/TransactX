import React, { useRef, useState } from "react";
import { motion, useReducedMotion } from "motion/react";
import { useAudioEngine } from "./AudioEngine";

export function MagneticButton({
  href,
  onClick,
  children,
  className = "",
  variant = "primary",
  ariaLabel,
  type = "button",
  disabled = false,
}: {
  href?: string;
  onClick?: (e: React.MouseEvent<HTMLElement>) => void;
  children: React.ReactNode;
  className?: string;
  variant?: "primary" | "secondary" | "ghost";
  ariaLabel?: string;
  type?: "button" | "submit" | "reset";
  disabled?: boolean;
}) {
  const reduce = useReducedMotion();
  const { play } = useAudioEngine();
  const ref = useRef<HTMLDivElement | null>(null);
  const [offset, setOffset] = useState({ x: 0, y: 0 });

  const handleMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (reduce || disabled || e.pointerType === "touch") return;
    const node = ref.current;
    if (!node) return;
    const rect = node.getBoundingClientRect();
    const dx = e.clientX - (rect.left + rect.width / 2);
    const dy = e.clientY - (rect.top + rect.height / 2);
    setOffset({
      x: Math.max(-7, Math.min(7, dx * 0.16)),
      y: Math.max(-5, Math.min(5, dy * 0.16)),
    });
  };

  const reset = () => setOffset({ x: 0, y: 0 });

  const baseStyles =
    variant === "primary"
      ? "bg-[var(--action-primary-bg)] !text-[var(--action-primary-fg)] shadow-[var(--shadow-subtle)] hover:shadow-[var(--shadow-raised)]"
      : variant === "secondary"
      ? "border border-[var(--border-hairline)] bg-[var(--bg-surface)] text-[var(--text-primary)] hover:border-[var(--border-active)] hover:bg-[var(--bg-surface-raised)]"
      : "text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)]/60";

  const sharedClass = `tx-shine-btn group/mag relative inline-flex items-center justify-center gap-2 rounded-lg px-5 py-3 text-sm font-semibold no-underline overflow-hidden transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] ${
    disabled ? "opacity-50 pointer-events-none" : "cursor-pointer"
  } ${baseStyles} ${className}`;

  const content = (
    <>
      {variant === "primary" && (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 -translate-x-full group-hover/mag:translate-x-full transition-transform duration-700 bg-gradient-to-r from-transparent via-white/20 to-transparent"
        />
      )}
      <span className="relative z-10 inline-flex items-center gap-2">{children}</span>
    </>
  );

  return (
    <motion.div
      ref={ref}
      onPointerMove={handleMove}
      onPointerLeave={reset}
      animate={reduce ? undefined : { x: offset.x, y: offset.y }}
      whileTap={reduce || disabled ? undefined : { scale: 0.97 }}
      transition={{ type: "spring", stiffness: 280, damping: 20, mass: 0.5 }}
      className="inline-flex"
    >
      {href ? (
        <a
          href={href}
          aria-label={ariaLabel}
          onClick={(e) => {
            play("tick");
            onClick?.(e);
          }}
          className={sharedClass}
        >
          {content}
        </a>
      ) : (
        <button
          type={type}
          disabled={disabled}
          aria-label={ariaLabel}
          onClick={(e) => {
            play("tick");
            onClick?.(e);
          }}
          className={sharedClass}
        >
          {content}
        </button>
      )}
    </motion.div>
  );
}
