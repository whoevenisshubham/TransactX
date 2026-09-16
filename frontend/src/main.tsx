import { StrictMode, FormEvent, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

type User = {
  id: string;
  name: string;
  paymentIdentifier: string;
  role: string;
};

type Account = {
  id: string;
  accountNumber: string;
  balancePaise: number;
  status: string;
};

type Recipient = {
  name: string;
  paymentIdentifier: string;
  status: string;
};

type ApiResponse<T> = { requestId: string; data: T };
type ApiErrorResponse = { requestId: string; error?: { code: string; message: string } };

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

async function apiRequest<T>(path: string, options: RequestInit = {}, token?: string): Promise<T> {
  const response = await fetch(`${apiBaseUrl}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...options.headers,
    },
  });
  const body = (await response.json()) as ApiResponse<T> | ApiErrorResponse;
  if (!response.ok) {
    throw new Error("error" in body && body.error ? body.error.message : "Request failed");
  }
  return (body as ApiResponse<T>).data;
}

function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem("transactx.token"));
  const [mode, setMode] = useState<"login" | "register">("login");
  const [message, setMessage] = useState("");

  function authenticated(nextToken: string) {
    sessionStorage.setItem("transactx.token", nextToken);
    setToken(nextToken);
    setMessage("");
  }

  function logout() {
    sessionStorage.removeItem("transactx.token");
    setToken(null);
  }

  if (token) {
    return <CustomerShell token={token} onLogout={logout} />;
  }

  return (
    <main className="app-shell">
      <section className="brand-panel">
        <p className="eyebrow">Payment infrastructure research prototype</p>
        <h1>TransactX</h1>
        <p>Identity and account access for a simulated, resilient payment network.</p>
        <div className="status-chip"><span className="dot online" /> API-backed access</div>
      </section>
      <section className="form-panel">
        <div className="tabs">
          <button className={mode === "login" ? "active" : ""} onClick={() => setMode("login")}>Sign in</button>
          <button className={mode === "register" ? "active" : ""} onClick={() => setMode("register")}>Create account</button>
        </div>
        {mode === "login" ? <LoginForm onAuthenticated={authenticated} setMessage={setMessage} /> : <RegisterForm onAuthenticated={authenticated} setMessage={setMessage} />}
        {message && <p className="error" role="alert">{message}</p>}
      </section>
    </main>
  );
}

function LoginForm({ onAuthenticated, setMessage }: { onAuthenticated: (token: string) => void; setMessage: (message: string) => void }) {
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      const data = await apiRequest<{ token: string }>("/api/auth/login", { method: "POST", body: JSON.stringify({ identifier, password }) });
      onAuthenticated(data.token);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Unable to sign in");
    } finally {
      setBusy(false);
    }
  }

  return <form onSubmit={submit}>
    <p className="eyebrow">Welcome back</p>
    <h2>Sign in to your account</h2>
    <label>Phone or payment identifier<input value={identifier} onChange={(event) => setIdentifier(event.target.value)} required /></label>
    <label>Password<input type="password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
    <button className="primary" disabled={busy}>{busy ? "Checking..." : "Sign in"}</button>
  </form>;
}

function RegisterForm({ onAuthenticated, setMessage }: { onAuthenticated: (token: string) => void; setMessage: (message: string) => void }) {
  const [form, setForm] = useState({ name: "", phone: "", paymentIdentifier: "", password: "", role: "CUSTOMER" });
  const [busy, setBusy] = useState(false);

  function update(field: keyof typeof form, value: string) { setForm((current) => ({ ...current, [field]: value })); }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      await apiRequest("/api/auth/register", { method: "POST", body: JSON.stringify(form) });
      const login = await apiRequest<{ token: string }>("/api/auth/login", { method: "POST", body: JSON.stringify({ identifier: form.paymentIdentifier, password: form.password }) });
      onAuthenticated(login.token);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Unable to create account");
    } finally {
      setBusy(false);
    }
  }

  return <form onSubmit={submit}>
    <p className="eyebrow">New identity</p>
    <h2>Create your TransactX account</h2>
    <label>Name<input value={form.name} onChange={(event) => update("name", event.target.value)} required /></label>
    <label>Phone<input value={form.phone} onChange={(event) => update("phone", event.target.value)} required /></label>
    <label>Payment identifier<input value={form.paymentIdentifier} onChange={(event) => update("paymentIdentifier", event.target.value)} placeholder="you@transactx" required /></label>
    <label>Password<input type="password" minLength={8} value={form.password} onChange={(event) => update("password", event.target.value)} required /></label>
    <label>Account type<select value={form.role} onChange={(event) => update("role", event.target.value)}><option value="CUSTOMER">Customer</option><option value="MERCHANT">Merchant</option></select></label>
    <button className="primary" disabled={busy}>{busy ? "Creating..." : "Create account"}</button>
  </form>;
}

function CustomerShell({ token, onLogout }: { token: string; onLogout: () => void }) {
  const [user, setUser] = useState<User | null>(null);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [recipient, setRecipient] = useState<Recipient | null>(null);
  const [lookup, setLookup] = useState("");
  const [message, setMessage] = useState("");

  useEffect(() => {
    Promise.all([apiRequest<User>("/api/me", {}, token), apiRequest<Account[]>("/api/accounts", {}, token)])
      .then(([profile, accountList]) => { setUser(profile); setAccounts(accountList); })
      .catch(() => onLogout());
  }, [token, onLogout]);

  async function resolveRecipient(event: FormEvent) {
    event.preventDefault();
    setMessage("");
    try {
      setRecipient(await apiRequest<Recipient>(`/api/recipients/${encodeURIComponent(lookup)}`, {}, token));
    } catch (error) {
      setRecipient(null);
      setMessage(error instanceof Error ? error.message : "Recipient not found");
    }
  }

  return <main className="dashboard">
    <header><div><p className="eyebrow">Authenticated customer shell</p><h1>Hello, {user?.name ?? "there"}</h1></div><button className="ghost" onClick={onLogout}>Log out</button></header>
    <section className="account-grid">{accounts.map((account) => <article className="account-card" key={account.id}><p className="eyebrow">Available balance</p><strong>₹{(account.balancePaise / 100).toFixed(2)}</strong><span>{account.accountNumber} · {account.status}</span></article>)}</section>
    <section className="profile-row"><div><span className="label">Payment identifier</span><strong>{user?.paymentIdentifier}</strong></div><div><span className="label">Role</span><strong>{user?.role}</strong></div></section>
    <section className="lookup-panel"><p className="eyebrow">Recipient lookup</p><h2>Find a TransactX identity</h2><form onSubmit={resolveRecipient}><input value={lookup} onChange={(event) => setLookup(event.target.value)} placeholder="recipient@transactx" required /><button className="primary">Look up</button></form>{recipient && <div className="recipient-result"><strong>{recipient.name}</strong><span>{recipient.paymentIdentifier} · {recipient.status}</span></div>}{message && <p className="error" role="alert">{message}</p>}</section>
    <p className="boundary-note">Payment execution is not enabled in Phase 1B. This screen never mutates balances.</p>
  </main>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><App /></StrictMode>);
