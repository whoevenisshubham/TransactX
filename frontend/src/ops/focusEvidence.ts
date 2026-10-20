// Citation targets may live in progressively disclosed technical evidence.
export function focusEvidence(id: string): boolean {
  const target = document.getElementById(id);
  if (!target) return false;
  for (let ancestor = target.parentElement; ancestor; ancestor = ancestor.parentElement) {
    if (ancestor instanceof HTMLDetailsElement) ancestor.open = true;
  }
  target.tabIndex = -1;
  target.focus({ preventScroll: true });
  target.scrollIntoView({ behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth", block: "center" });
  return true;
}
