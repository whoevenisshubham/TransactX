import React, { createContext, useContext, useEffect, useState } from "react";

export type Theme = "light" | "dark" | "system";

type ThemeProviderState = {
  theme: Theme;
  resolvedTheme: "light" | "dark";
  setTheme: (theme: Theme) => void;
};

export const THEME_STORAGE_KEY = "transactx.theme";
let memoryPreference: Theme | null = null;

export function readStoredTheme(key = THEME_STORAGE_KEY, defaultTheme: Theme = "system"): Theme {
  if (memoryPreference) return memoryPreference;
  if (typeof window === "undefined") return defaultTheme;
  try {
    const stored = localStorage.getItem(key) as Theme | null;
    if (stored === "light" || stored === "dark" || stored === "system") {
      return stored;
    }
  } catch {}
  return defaultTheme;
}

export function writeStoredTheme(theme: Theme, key = THEME_STORAGE_KEY): void {
  try {
    localStorage.setItem(key, theme);
    memoryPreference = null;
  } catch {
    memoryPreference = theme;
  }
}

export function resolveActiveTheme(theme: Theme): "light" | "dark" {
  if (theme === "light" || theme === "dark") return theme;
  if (typeof window !== "undefined" && typeof window.matchMedia === "function") {
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  return "light";
}

export function applyThemeToDocument(theme: Theme): "light" | "dark" {
  const activeTheme = resolveActiveTheme(theme);
  if (typeof document !== "undefined") {
    const root = document.documentElement;
    root.setAttribute("data-theme", activeTheme);
    root.style.colorScheme = activeTheme;
    if (activeTheme === "dark") {
      root.classList.add("dark");
    } else {
      root.classList.remove("dark");
    }
  }
  return activeTheme;
}

const ThemeContext = createContext<ThemeProviderState | undefined>(undefined);

export function ThemeProvider({
  children,
  defaultTheme = "system",
  storageKey = THEME_STORAGE_KEY,
}: {
  children: React.ReactNode;
  defaultTheme?: Theme;
  storageKey?: string;
}) {
  const [theme, setThemeState] = useState<Theme>(() => readStoredTheme(storageKey, defaultTheme));
  const [resolvedTheme, setResolvedTheme] = useState<"light" | "dark">(() =>
    resolveActiveTheme(readStoredTheme(storageKey, defaultTheme))
  );

  useEffect(() => {
    const syncTheme = () => {
      const current = readStoredTheme(storageKey, defaultTheme);
      setThemeState(current);
      setResolvedTheme(applyThemeToDocument(current));
    };
    window.addEventListener("transactx:theme", syncTheme);
    window.addEventListener("storage", syncTheme);
    return () => {
      window.removeEventListener("transactx:theme", syncTheme);
      window.removeEventListener("storage", syncTheme);
    };
  }, [storageKey, defaultTheme]);

  useEffect(() => {
    const syncAndApply = () => {
      setResolvedTheme(applyThemeToDocument(theme));
    };

    syncAndApply();

    if (theme === "system" && typeof window !== "undefined" && typeof window.matchMedia === "function") {
      const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
      mediaQuery.addEventListener("change", syncAndApply);
      return () => mediaQuery.removeEventListener("change", syncAndApply);
    }
  }, [theme]);

  const setTheme = (newTheme: Theme) => {
    writeStoredTheme(newTheme, storageKey);
    setThemeState(newTheme);
    setResolvedTheme(applyThemeToDocument(newTheme));
    window.dispatchEvent(new Event("transactx:theme"));
  };

  return (
    <ThemeContext.Provider value={{ theme, resolvedTheme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme(): ThemeProviderState {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return context;
}
