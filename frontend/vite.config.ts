import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

export default defineConfig({
  plugins: [react(), {
    name: "transactx-offline-assets",
    apply: "build",
    generateBundle(_, bundle) {
      const assets = Object.keys(bundle).filter(name => name.startsWith("assets/") && !name.endsWith(".map")).sort().map(name => `/${name}`);
      this.emitFile({ type: "asset", fileName: "offline-assets.json", source: JSON.stringify(assets) });
    },
    writeBundle(options) {
      const directory = options.dir ?? "dist";
      const manifest = readFileSync(resolve(directory, "offline-assets.json"), "utf8");
      const revision = createHash("sha256").update(manifest).digest("hex").slice(0, 16);
      const worker = resolve(directory, "sw.js");
      writeFileSync(worker, readFileSync(worker, "utf8").replace("__STATIC_REVISION__", revision));
    },
  }],
});
