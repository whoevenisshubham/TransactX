import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

function App() {
  const [apiStatus, setApiStatus] = useState("Checking API...");

  useEffect(() => {
    fetch(`${apiBaseUrl}/health`)
      .then((response) => {
        if (!response.ok) throw new Error("API request failed");
        return response.json();
      })
      .then(() => setApiStatus("API connected"))
      .catch(() => setApiStatus("API unavailable"));
  }, []);

  return (
    <main className="shell">
      <p className="eyebrow">Phase 0 foundation</p>
      <h1>TransactX</h1>
      <p className="subtitle">A resilient payment infrastructure research prototype.</p>
      <div className="status" role="status">
        <span className={apiStatus === "API connected" ? "dot online" : "dot"} />
        {apiStatus}
      </div>
    </main>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);