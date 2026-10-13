import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseOperationalEventStream, subscribeOperationalEvents, type OperationalHint } from "./operationalEvents";

describe("operational SSE recovery", () => {
  it("aborts an open stream offline and reconnects only when online", async () => {
    const events = new EventTarget();
    let isOnline = true;
    let calls = 0;
    let signal: AbortSignal | undefined;
    const statuses: string[] = [];
    const hints: OperationalHint[] = [];
    const stop = subscribeOperationalEvents("token", (hint) => hints.push(hint), (status) => statuses.push(status), {
      network: { events, isOnline: () => isOnline }, reconnectDelayMs: 0,
      fetchImpl: async (_url, init) => {
        calls += 1;
        signal = init?.signal ?? undefined;
        return new Response(new ReadableStream({ start(controller) {
          controller.enqueue(new TextEncoder().encode('event: ready\ndata: {"topics":["all"]}\n\n'));
          signal?.addEventListener("abort", () => controller.error(new Error("aborted")), { once: true });
        } }));
      },
    });
    try {
      await new Promise((resolve) => setTimeout(resolve, 10));
      assert.equal(statuses.at(-1), "connected");
      isOnline = false;
      events.dispatchEvent(new Event("offline"));
      assert.equal(signal?.aborted, true);
      await new Promise((resolve) => setTimeout(resolve, 20));
      assert.equal(statuses.at(-1), "disconnected");
      assert.equal(calls, 1);
      isOnline = true;
      events.dispatchEvent(new Event("online"));
      await new Promise((resolve) => setTimeout(resolve, 10));
      assert.equal(calls, 2);
      assert.equal(statuses.at(-1), "connected");
      assert.equal(hints.filter((hint) => hint.event === "ready").length, 2);
    } finally { stop(); }
    events.dispatchEvent(new Event("online"));
    assert.equal(calls, 2, "closed subscriptions cannot reconnect");
  });

  it("clears a rejected session without retrying or issuing stale-data hints", async () => {
    let calls = 0;
    let rejected = 0;
    const hints: OperationalHint[] = [];
    let stop = () => {};
    await new Promise<void>((resolve) => {
      stop = subscribeOperationalEvents("expired-token", (hint) => hints.push(hint), () => {}, {
        fetchImpl: async () => { calls += 1; return new Response(null, { status: 401 }); },
        reconnectDelayMs: 0,
        onUnauthorized: () => { rejected += 1; resolve(); },
      });
    });
    await new Promise((resolve) => setTimeout(resolve, 20));
    stop();
    assert.equal(calls, 1);
    assert.equal(rejected, 1);
    assert.deepEqual(hints, []);
  });

  it("parses ready, invalidation, heartbeat, and partial frames", () => {
    const parsed = parseOperationalEventStream(
      ': heartbeat\n\nevent: ready\ndata: {"topics":["all"],"reason":"connected"}\n\n' +
      'event: invalidate\ndata: {"topics":["health","routing"],"reason":"durable_state_changed"}\n\n' +
      'event: stale\ndata: {"topics"',
    );
    assert.deepEqual(parsed.events.map((event) => [event.event, event.topics]), [
      ["ready", ["all"]],
      ["invalidate", ["health", "routing"]],
    ]);
    assert.match(parsed.remainder, /event: stale/);
  });

  it("refetches on initial connection, disconnect gap, and reconnect", async () => {
    let calls = 0;
    const encoder = new TextEncoder();
    const fakeFetch: typeof fetch = async () => {
      calls += 1;
      const payload = calls === 1
        ? 'event: ready\ndata: {"topics":["all"],"reason":"connected"}\n\n'
        : 'event: ready\ndata: {"topics":["all"],"reason":"connected"}\n\nevent: stale\ndata: {"topics":["all"],"reason":"source_unavailable"}\n\n';
      return new Response(new ReadableStream({
        start(controller) {
          controller.enqueue(encoder.encode(payload));
          controller.close();
        },
      }), { status: 200, headers: { "Content-Type": "text/event-stream" } });
    };

    const hints: OperationalHint[] = [];
    let stop = () => {};
    await new Promise<void>((resolve) => {
      stop = subscribeOperationalEvents("token", (hint) => {
        hints.push(hint);
        if (calls >= 2 && hint.event === "stale") resolve();
      }, () => {}, { fetchImpl: fakeFetch, reconnectDelayMs: 0, staleAfterMs: 1000 });
    });
    stop();

    assert.ok(calls >= 2, "stream reconnects after EOF");
    assert.ok(hints.some((hint) => hint.event === "ready"), "initial/reconnect performs authoritative refetch");
    assert.ok(hints.some((hint) => hint.event === "gap"), "disconnect performs gap recovery refetch");
    assert.ok(hints.some((hint) => hint.event === "stale"), "stale indication performs authoritative refetch");
  });
});
