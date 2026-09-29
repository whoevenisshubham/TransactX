const apiBaseUrl = (typeof import.meta !== "undefined" && (import.meta as any)?.env?.VITE_API_BASE_URL) || "http://localhost:8080";

export type OperationalHint = {
  event: "ready" | "invalidate" | "stale" | "gap";
  topics: string[];
  reason: string;
  sequence?: number;
};

export type StreamStatus = "connecting" | "connected" | "disconnected";

type ParsedStream = { events: OperationalHint[]; remainder: string };

export function parseOperationalEventStream(input: string): ParsedStream {
  const normalized = input.replace(/\r\n/g, "\n");
  const blocks = normalized.split("\n\n");
  const remainder = blocks.pop() ?? "";
  const events: OperationalHint[] = [];

  for (const block of blocks) {
    let eventName = "message";
    const dataLines: string[] = [];
    for (const line of block.split("\n")) {
      if (line.startsWith("event:")) eventName = line.slice(6).trim();
      if (line.startsWith("data:")) dataLines.push(line.slice(5).trimStart());
    }
    if (!dataLines.length || !["ready", "invalidate", "stale"].includes(eventName)) continue;
    try {
      const payload = JSON.parse(dataLines.join("\n")) as Omit<OperationalHint, "event">;
      events.push({
        event: eventName as OperationalHint["event"],
        topics: Array.isArray(payload.topics) ? payload.topics : ["all"],
        reason: payload.reason || eventName,
        sequence: payload.sequence,
      });
    } catch {
      events.push({ event: "stale", topics: ["all"], reason: "invalid_stream_payload" });
    }
  }
  return { events, remainder };
}

type SubscribeOptions = {
  fetchImpl?: typeof fetch;
  reconnectDelayMs?: number;
  staleAfterMs?: number;
};

// The stream carries invalidation hints only. Callers refetch authoritative
// HTTP resources for every ready, stale, gap, or relevant invalidation event.
export function subscribeOperationalEvents(
  token: string,
  onHint: (hint: OperationalHint) => void,
  onStatus: (status: StreamStatus) => void,
  options: SubscribeOptions = {},
): () => void {
  const fetchImpl = options.fetchImpl ?? fetch;
  const reconnectDelayMs = options.reconnectDelayMs ?? 1000;
  const staleAfterMs = options.staleAfterMs ?? 35000;
  let stopped = false;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let watchdogTimer: ReturnType<typeof setTimeout> | undefined;
  let activeController: AbortController | undefined;

  const clearWatchdog = () => {
    if (watchdogTimer !== undefined) clearTimeout(watchdogTimer);
    watchdogTimer = undefined;
  };

  const armWatchdog = () => {
    clearWatchdog();
    watchdogTimer = setTimeout(() => activeController?.abort("stream_stale"), staleAfterMs);
  };

  const connect = async () => {
    if (stopped) return;
    onStatus("connecting");
    const controller = new AbortController();
    activeController = controller;
    try {
      const response = await fetchImpl(`${apiBaseUrl}/api/ops/events/stream`, {
        headers: { Accept: "text/event-stream", Authorization: `Bearer ${token}` },
        cache: "no-store",
        signal: controller.signal,
      });
      if (!response.ok || !response.body) throw new Error(`stream status ${response.status}`);
      onStatus("connected");
      armWatchdog();
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      while (!stopped) {
        const { done, value } = await reader.read();
        if (done) break;
        armWatchdog();
        buffer += decoder.decode(value, { stream: true });
        const parsed = parseOperationalEventStream(buffer);
        buffer = parsed.remainder;
        for (const hint of parsed.events) onHint(hint);
      }
    } catch {
      // The gap handler below performs the authoritative recovery read.
    } finally {
      clearWatchdog();
      if (activeController === controller) activeController = undefined;
      if (!stopped) {
        onStatus("disconnected");
        onHint({ event: "gap", topics: ["all"], reason: "stream_disconnected" });
        reconnectTimer = setTimeout(() => void connect(), reconnectDelayMs);
      }
    }
  };

  void connect();
  return () => {
    stopped = true;
    clearWatchdog();
    if (reconnectTimer !== undefined) clearTimeout(reconnectTimer);
    activeController?.abort("subscription_closed");
  };
}
