const SHELL_CACHE = "transactx-static-shell-__STATIC_REVISION__";

async function storeDecoded(cache, key, response) {
  // Cache Storage holds decoded bytes; retaining the wire encoding makes
  // module and stylesheet loads fail when the shell is served offline.
  const headers = new Headers();
  for (const name of [
    "content-type", "content-security-policy", "cross-origin-embedder-policy",
    "cross-origin-opener-policy", "cross-origin-resource-policy", "referrer-policy",
    "x-content-type-options", "x-frame-options", "permissions-policy",
  ]) {
    const value = response.headers.get(name);
    if (value) headers.set(name, value);
  }
  if (!headers.has("content-type")) headers.set("content-type", "application/octet-stream");
  await cache.put(key, new Response(await response.clone().arrayBuffer(), {
    status: response.status,
    statusText: response.statusText,
    headers,
  }));
}

self.addEventListener("install", (event) => {
  event.waitUntil((async () => {
    const response = await fetch(new Request("/", { cache: "reload" }));
    if (!response.ok) throw new Error("Static shell is unavailable");
    const manifest = await fetch(new Request("/offline-assets.json", { cache: "reload" }));
    if (!manifest.ok) throw new Error("Static asset manifest is unavailable");
    const assets = await manifest.json();
    if (!Array.isArray(assets) || assets.length > 200 || assets.some(asset => typeof asset !== "string" || !/^\/assets\/[a-zA-Z0-9_./-]+$/.test(asset) || asset.includes(".."))) throw new Error("Invalid static asset manifest");
    const cache = await caches.open(SHELL_CACHE);
    await storeDecoded(cache, "/", response);
    await Promise.all(assets.map(async (asset) => {
      const resource = await fetch(new Request(asset, { cache: "reload" }));
      if (!resource.ok) throw new Error(`Static asset unavailable: ${asset}`);
      await storeDecoded(cache, asset, resource);
    }));
    // An update waits for existing tabs to close. Never take over a payment in
    // progress or reload its JavaScript to activate a new visual release.
  })());
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const names = await caches.keys();
    await Promise.all(names.filter((name) => name.startsWith("transactx-static-shell-") && name !== SHELL_CACHE).map((name) => caches.delete(name)));
    await self.clients.claim();
  })());
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== "GET" || url.origin !== self.location.origin) return;
  if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/auth/")) return;
  if (request.mode === "navigate") {
    event.respondWith(fetch(request).then(async (response) => {
      if (response.ok) await storeDecoded(await caches.open(SHELL_CACHE), "/", response);
      return response;
    }).catch(async () => (await caches.open(SHELL_CACHE)).match("/") ?? Response.error()));
  } else if (url.pathname.startsWith("/assets/")) {
    event.respondWith((async () => {
      const cache = await caches.open(SHELL_CACHE);
      const saved = await cache.match(request);
      if (saved) return saved;
      const response = await fetch(request);
      if (response.ok) await storeDecoded(cache, request, response);
      return response;
    })());
  }
});
