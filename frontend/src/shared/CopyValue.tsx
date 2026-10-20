import { useState } from "react";

export function CopyValue({ value, label }: { value: string; label: string }) {
  const [status, setStatus] = useState("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setStatus("Copied");
    } catch { setStatus("Copy unavailable. Select the value to copy."); }
  }
  return <span className="copy-value"><button type="button" className="button button-secondary button-sm" onClick={() => void copy()}>{label}</button><span role="status">{status}</span></span>;
}
