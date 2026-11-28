import React, { useEffect, useState } from "react";
import { motion, useReducedMotion, useSpring } from "motion/react";

export function CursorFollower() {
  const reduce = useReducedMotion();
  const [enabled, setEnabled] = useState(false);
  const [visible, setVisible] = useState(false);
  const [label, setLabel] = useState<string | null>(null);

  const springX = useSpring(-100, { stiffness: 360, damping: 32, mass: 0.45 });
  const springY = useSpring(-100, { stiffness: 360, damping: 32, mass: 0.45 });

  useEffect(() => {
    if (reduce || typeof window === "undefined") {
      setEnabled(false);
      return;
    }
    const finePointer = window.matchMedia?.("(pointer: fine)")?.matches;
    if (!finePointer) {
      setEnabled(false);
      return;
    }
    setEnabled(true);

    const onMove = (e: PointerEvent) => {
      if (e.pointerType !== "mouse") return;
      springX.set(e.clientX);
      springY.set(e.clientY);
      if (!visible) setVisible(true);
      const rawTarget = e.target as HTMLElement | null;
      if (rawTarget?.closest?.("button, a, input, textarea, select, [role='button'], [role='tab']")) {
        setLabel(null);
        return;
      }
      const target = rawTarget?.closest?.("[data-cursor]");
      const nextLabel = target?.getAttribute("data-cursor") ?? null;
      setLabel(nextLabel);
    };

    const onLeave = () => setVisible(false);

    window.addEventListener("pointermove", onMove, { passive: true });
    document.addEventListener("mouseleave", onLeave);
    return () => {
      window.removeEventListener("pointermove", onMove);
      document.removeEventListener("mouseleave", onLeave);
    };
  }, [reduce, springX, springY, visible]);

  if (!enabled || !visible || !label) return null;

  return (
    <motion.div
      aria-hidden="true"
      style={{
        x: springX,
        y: springY,
        translateX: "14px",
        translateY: "14px",
      }}
      className="fixed top-0 left-0 z-[60] pointer-events-none hidden lg:flex items-center justify-center"
    >
      <span className="rounded-full border border-[var(--blueprint-ice,#93b4ff)] bg-[#0b1222]/90 px-2.5 py-0.5 font-mono text-[10px] font-semibold tracking-wide text-[#f8fafc] shadow-lg backdrop-blur-sm">
        {label}
      </span>
    </motion.div>
  );
}
