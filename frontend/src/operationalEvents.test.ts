import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseOperationalEventStream, subscribeOperationalEvents, type OperationalHint } from "./operationalEvents";

describe("operational SSE recovery", () => {
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
