import { useEffect, useState } from "react";

export type ThemePreference = "system" | "light" | "dark";
const key = "transactx.theme";
let transientPreference: ThemePreference | null = null;

function readPreference(): ThemePreference {
  if (transientPreference) return transientPreference;
  try {
    const saved = localStorage.getItem(key);
    return saved === "light" || saved === "dark" ? saved : "system";
  } catch { return "system"; }
}

export function ThemeSelect() {
  const [preference, setPreference] = useState<ThemePreference>(readPreference);
  useEffect(() => {
    const sync = () => setPreference(readPreference());
    window.addEventListener("transactx:theme", sync);
    window.addEventListener("storage", sync);
    return () => { window.removeEventListener("transactx:theme", sync); window.removeEventListener("storage", sync); };
  }, []);
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const theme = preference === "system" ? media.matches ? "dark" : "light" : preference;
      document.documentElement.dataset.theme = theme;
      document.documentElement.style.colorScheme = theme;
    };
    apply();
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [preference]);
  return <label className="theme-select"><span>Appearance</span><select aria-label="Appearance" value={preference} onChange={(event) => {
    const next = event.target.value as ThemePreference;
    try { localStorage.setItem(key, next); transientPreference = null; } catch { transientPreference = next; }
    setPreference(next);
    window.dispatchEvent(new Event("transactx:theme"));
  }}><option value="system">System</option><option value="light">Light</option><option value="dark">Dark</option></select></label>;
}
