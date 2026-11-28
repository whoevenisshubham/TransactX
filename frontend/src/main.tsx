import { lazy, Suspense, StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MotionConfig } from "motion/react";
import { Toaster } from "sonner";
import { api, ApiError } from "./api";
import { Amount, BrandMark, Button } from "./components";
import { OfflineIntentQueue, type OfflineIntent } from "./offlineQueue";
import type { User } from "./types";
import { LoadingShell } from "./shared/ShellStates";
import { LandingPage } from "./features/public";
import { ThemeProvider, useTheme } from "./components/theme/ThemeProvider";
import { RootLayout } from "./components/layout/RootLayout";
import { AudioProvider, CelebrationOverlay, CommandPalette, CursorFollower } from "./components/effects";
import "./styles/globals.css";
import "./styles.css";
import "./design/ops.css";
import "./design/experience.css";
import "./design/visual-reset.css";

export { parsePaise } from "./money";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: false, // Critical invariant: zero automatic retry for financial mutations
    },
  },
});

const CustomerShell = lazy(() => import("./customer/CustomerShell").then(module => ({ default: module.CustomerShell })));
const MerchantShell = lazy(() => import("./merchant/MerchantShell").then(module => ({ default: module.MerchantShell })));
const ConsoleShell = lazy(() => import("./console").then(module => ({ default: module.ConsoleShell })));
const ShowcasePage = lazy(() => import("./showcase/ShowcasePage").then(module => ({ default: module.ShowcasePage })));
const GoogleCallbackPage = lazy(() => import("./features/auth/GoogleCallbackPage").then(module => ({ default: module.GoogleCallbackPage })));
const AuthPage = lazy(() => import("./features/auth").then(module => ({ default: module.AuthPage })));
const DemoPage = lazy(() => import("./features/demo").then(module => ({ default: module.DemoPage })));
const PrivacyPage = lazy(() => import("./features/public/PrivacyPage").then(module => ({ default: module.PrivacyPage })));
const ComponentShowcase = lazy(() => import("./showcase/ComponentShowcase").then(module => ({ default: module.ComponentShowcase })));

type OfflineOwner = Pick<User, "id" | "role">;

function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem("transactx.token"));
  const [authMode, setAuthMode] = useState<"login" | "register">("login");
  const [user, setUser] = useState<User | null>(null);
  const [offlineUser, setOfflineUser] = useState<OfflineOwner | null>(null);
  const [offlineMode, setOfflineMode] = useState(false);
  const [roleLoading, setRoleLoading] = useState(false);

  // When a token arrives, fetch the user's role from the server.
  useEffect(() => {
    if (["/showcase","/auth/callback"].includes(window.location.pathname)) return;
    if (!token) { setUser(null); setOfflineUser(null); setOfflineMode(false); return; }
    let active = true;
    let generation = 0;
    const verify = () => {
      const current = ++generation;
      setRoleLoading(true);
      api.me(token).then((profile) => {
        if (!active || current !== generation) return;
        const path = window.location.pathname;
        const allowedPath = path === "/design-system" || (profile.role === "CUSTOMER" ? path === "/" || /^\/(pay|transactions)(\/|$)/.test(path) : profile.role === "MERCHANT" ? /^\/merchant(\/|$)/.test(path) : /^\/console(\/|$)/.test(path));
        if (!allowedPath) window.history.replaceState({}, "", profile.role === "CUSTOMER" ? "/" : profile.role === "MERCHANT" ? "/merchant" : "/console/worklist");
        sessionStorage.setItem("transactx.profile", JSON.stringify({ token, profile: { id: profile.id, role: profile.role } }));
        setUser(profile);
        setOfflineUser(null);
        setOfflineMode(false);
        setRoleLoading(false);
      }).catch((caught) => {
        if (!active || current !== generation) return;
        if (caught instanceof ApiError && (caught.status === 0 || caught.status >= 500)) {
          const cached = readCachedProfile(token);
          setOfflineUser(cached);
          setOfflineMode(true);
        } else {
          sessionStorage.removeItem("transactx.token");
          sessionStorage.removeItem("transactx.profile");
          window.history.replaceState({}, "", "/login");
          setToken(null);
          setUser(null);
          setOfflineUser(null);
          setOfflineMode(false);
        }
        setRoleLoading(false);
      });
    };
    verify();
    window.addEventListener("online", verify);
    return () => { active = false; window.removeEventListener("online", verify); };
  }, [token]);

  function authenticated(nextToken: string) {
    queryClient.clear();
    sessionStorage.setItem("transactx.token", nextToken);
    setToken(nextToken);
  }
  function logout() {
    queryClient.clear();
    sessionStorage.removeItem("transactx.token");
    sessionStorage.removeItem("transactx.profile");
    window.history.replaceState({}, "", "/login");
    setToken(null);
    setUser(null);
    setOfflineUser(null);
    setOfflineMode(false);
  }

  if (window.location.pathname === "/auth/callback") return <Suspense fallback={<LoadingShell />}><GoogleCallbackPage onAuthenticated={authenticated} /></Suspense>;
  if (window.location.pathname === "/design-system") return <Suspense fallback={<LoadingShell />}><RootLayout role={user?.role ?? "CUSTOMER"} userName={user?.name} onLogout={token ? logout : undefined}><ComponentShowcase /></RootLayout></Suspense>;
  if (window.location.pathname === "/showcase") return <Suspense fallback={<LoadingShell />}><ShowcasePage /></Suspense>;
  if (window.location.pathname === "/privacy") return <Suspense fallback={<LoadingShell />}><PrivacyPage /></Suspense>;
  if (!token && window.location.pathname === "/") return <LandingPage />;
  if (!token && window.location.pathname === "/demo") return <Suspense fallback={<LoadingShell />}><DemoPage onAuthenticated={authenticated} /></Suspense>;
  if (!token) return <Suspense fallback={<LoadingShell />}><AuthPage mode={authMode} onModeChange={setAuthMode} onAuthenticated={authenticated} /></Suspense>;
  if (offlineMode) return <OfflineRecoveryShell user={offlineUser} onLogout={logout} onRetry={() => window.dispatchEvent(new Event("online"))} />;
  if (roleLoading || !user) return <LoadingShell />;

  if (user.role === "OPS_ADMIN" || user.role === "OPS_VIEWER") return <Suspense fallback={<LoadingShell />}><ConsoleShell token={token} user={user} onLogout={logout} /></Suspense>;
  if (user.role === "MERCHANT") return <Suspense fallback={<LoadingShell />}><MerchantShell token={token} user={user} onLogout={logout} /></Suspense>;
  if (user.role === "CUSTOMER") return <Suspense fallback={<LoadingShell />}><CustomerShell token={token} onLogout={logout} /></Suspense>;
  // Unknown roles — access denied screen
  return (
    <main className="center-state" style={{ flexDirection: "column", paddingTop: "10vh" }}>
      <p className="eyebrow">Access denied</p>
      <h1 style={{ fontSize: "1.8rem" }}>This area is not available</h1>
      <p style={{ color: "var(--muted)", maxWidth: 360, textAlign: "center" }}>
        Your account role does not have access to this product. Please contact your administrator.
      </p>
      <Button onClick={logout}>Log out</Button>
    </main>
  );
}

function readCachedProfile(token: string): OfflineOwner | null {
  try {
    const cached = JSON.parse(sessionStorage.getItem("transactx.profile") ?? "null") as { token?: string; profile?: OfflineOwner } | null;
    if (cached?.token !== token || cached.profile?.role !== "CUSTOMER" || typeof cached.profile.id !== "string") return null;
    return cached.profile;
  } catch {
    return null;
  }
}

function OfflineRecoveryShell({ user, onLogout, onRetry }: { user: OfflineOwner | null; onLogout: () => void; onRetry: () => void }) {
  const [intents, setIntents] = useState<OfflineIntent[]>([]);
  const [queueError, setQueueError] = useState(false);

  useEffect(() => {
    if (!user) return;
    let active = true;
    const queue = new OfflineIntentQueue(undefined, user.id);
    const states: OfflineIntent["state"][] = ["QUEUED", "SYNCING", "RETRYABLE", "FAILED"];
    Promise.all(states.map((state) => queue.listByState(state))).then((groups) => {
      if (active) setIntents(groups.flat().sort((left, right) => right.updatedAt.localeCompare(left.updatedAt)));
    }).catch(() => { if (active) setQueueError(true); });
    return () => { active = false; void queue.close(); };
  }, [user]);

  return (
    <main className="offline-recovery">
      <div className="brand-lockup"><BrandMark /><span>TransactX</span></div>
      <p className="eyebrow">Connection unavailable</p>
      <h1>You're offline</h1>
      <p>Payment status and account details cannot be verified until the API is reachable. Saved intents will be checked after your session is verified again.</p>
      {user && (
        <section className="offline-queue-panel" aria-label="Saved local intents">
          <h2>Offline payment intents</h2>
          {queueError ? <p>Local payment intents could not be read on this device.</p> :
            intents.length === 0 ? <p>No unsynced payment intents are stored for this account.</p> :
              <ul>{intents.map((intent) => <li key={intent.clientRequestId}>{intent.payload.recipient} · <Amount paise={intent.payload.amountPaise} /> · {intent.state.toLowerCase()}</li>)}</ul>}
        </section>
      )}
      <div className="offline-recovery-actions">
        <Button onClick={onRetry}>Try again</Button>
        <Button variant="secondary" onClick={onLogout}>Log out</Button>
      </div>
    </main>
  );
}

function ThemedToaster() {
  const { resolvedTheme } = useTheme();
  return (
    <Toaster
      position="bottom-right"
      theme={resolvedTheme}
      closeButton
      toastOptions={{
        style: {
          background: "var(--bg-surface)",
          color: "var(--text-primary)",
          border: "1px solid var(--border-active)",
          boxShadow: "var(--shadow-raised)",
          fontFamily: "var(--font-sans)",
        },
      }}
    />
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <ThemeProvider>
          <MotionConfig reducedMotion="user">
            <AudioProvider>
              <App />
              <CommandPalette />
              <CelebrationOverlay />
              <CursorFollower />
              <ThemedToaster />
            </AudioProvider>
          </MotionConfig>
        </ThemeProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>
);

if (import.meta.env.PROD && "serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("/sw.js").catch(() => {
      // The online app remains usable when offline support cannot be installed.
    });
  });
}
