import { useEffect, useState } from "react";
import {
  applyThemeToDocument,
  readStoredTheme,
  THEME_STORAGE_KEY,
  writeStoredTheme,
  type Theme,
} from "./components/theme/ThemeProvider";

export type ThemePreference = Theme;

/**
 * Unified accessible Appearance selector synced to ThemeProvider's `transactx.theme`
 * key and document `data-theme` / `.dark` attributes without external style drift.
 * Authenticated shells use `ThemeToggle`; public headers retain the `Appearance`
 * label control expected by existing browser specs.
 */
export function ThemeSelect() {
  const [preference, setPreference] = useState<ThemePreference>(() =>
    readStoredTheme(THEME_STORAGE_KEY, "system")
  );

  useEffect(() => {
    const sync = () => setPreference(readStoredTheme(THEME_STORAGE_KEY, "system"));
    window.addEventListener("transactx:theme", sync);
    window.addEventListener("storage", sync);
    return () => {
      window.removeEventListener("transactx:theme", sync);
      window.removeEventListener("storage", sync);
    };
  }, []);

  useEffect(() => {
    applyThemeToDocument(preference);
    if (preference !== "system") return;
    const media = matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => applyThemeToDocument("system");
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [preference]);

  return (
    <label className="theme-select inline-flex items-center gap-2 text-xs text-[var(--text-secondary)]">
      <span className="sr-only">Appearance</span>
      <select
        aria-label="Appearance"
        value={preference}
        onChange={(event) => {
          const next = event.target.value as ThemePreference;
          writeStoredTheme(next, THEME_STORAGE_KEY);
          setPreference(next);
          applyThemeToDocument(next);
          window.dispatchEvent(new Event("transactx:theme"));
        }}
        className="h-9 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-2.5 text-xs font-medium text-[var(--text-primary)] transition-colors hover:border-[var(--border-active)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] cursor-pointer"
      >
        <option value="system">System</option>
        <option value="light">Light</option>
        <option value="dark">Dark</option>
      </select>
    </label>
  );
}
