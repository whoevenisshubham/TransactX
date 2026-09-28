/**
 * qr.test.ts — QR encoder contract tests.
 *
 * Validates that encodeQRSvg and encodeQRSvgSync produce ISO 18004-compliant
 * QR codes whose decoded payload matches the original input exactly.
 *
 * Uses `jsQR` for decoding — a pure-JavaScript ISO 18004 decoder.
 * Scoped as a devDependency for test validation only.
 */
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import jsQR from "jsqr";
import { encodeQRSvg, encodeQRSvgSync } from "./qr.js";

/**
 * Helper: rasterize and decode a QR SVG string using jsQR without native canvas.
 * Handles both <path d="..."/> and <rect x="..." y="..."/> module formats.
 */
function decodeSvgQR(svg: string): string | null {
  const vbMatch = svg.match(/viewBox="0 0 (\d+) (\d+)"/);
  if (!vbMatch) {
    throw new Error("SVG missing viewBox");
  }
  const totalW = parseInt(vbMatch[1], 10);
  const totalH = parseInt(vbMatch[2], 10);

  // Initialize 2D grid: 0 = light/white, 1 = dark/black
  const grid = Array.from({ length: totalH }, () => new Uint8Array(totalW));

  // 1. Check for <path stroke="..." d="..."/> (used by qrcode toString)
  const dMatches = svg.matchAll(/d="([^"]+)"/g);
  for (const match of dMatches) {
    const d = match[1];
    // Tokenize SVG path commands: M x y, m dx 0, h len
    const tokenRegex = /([Mmh])([^Mmh]*)/g;
    let currX = 0;
    let currY = 0;
    let token: RegExpExecArray | null;
    while ((token = tokenRegex.exec(d)) !== null) {
      const cmd = token[1];
      const args = token[2].trim().split(/\s+/).map(Number);
      if (cmd === "M") {
        currX = args[0];
        currY = Math.floor(args[1]);
      } else if (cmd === "m") {
        currX += args[0];
      } else if (cmd === "h") {
        const len = args[0];
        for (let i = 0; i < len; i++) {
          if (currY >= 0 && currY < totalH && currX + i >= 0 && currX + i < totalW) {
            grid[currY][currX + i] = 1;
          }
        }
        currX += len;
      }
    }
  }

  // 2. Check for <rect x="..." y="..." width="1" height="1"/> (used by matrix renderer)
  const rectRegex = /<rect[^>]+x="([\d.]+)"[^>]+y="([\d.]+)"/g;
  let rMatch: RegExpExecArray | null;
  while ((rMatch = rectRegex.exec(svg)) !== null) {
    const x = Math.round(parseFloat(rMatch[1]));
    const y = Math.round(parseFloat(rMatch[2]));
    if (y >= 0 && y < totalH && x >= 0 && x < totalW) {
      grid[y][x] = 1;
    }
  }

  // Scale up to 10px per module for reliable jsQR optical recognition
  const scale = 10;
  const imgW = totalW * scale;
  const imgH = totalH * scale;
  const rgba = new Uint8ClampedArray(imgW * imgH * 4);
  rgba.fill(255); // White background with alpha = 255

  for (let r = 0; r < totalH; r++) {
    for (let c = 0; c < totalW; c++) {
      if (grid[r][c] === 1) {
        for (let dy = 0; dy < scale; dy++) {
          for (let dx = 0; dx < scale; dx++) {
            const px = c * scale + dx;
            const py = r * scale + dy;
            const idx = (py * imgW + px) * 4;
            rgba[idx] = 0;
            rgba[idx + 1] = 0;
            rgba[idx + 2] = 0;
            rgba[idx + 3] = 255;
          }
        }
      }
    }
  }

  const result = jsQR(rgba, imgW, imgH);
  return result ? result.data : null;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("QR encoder — ISO 18004 standards-compliant encoding & decoding", () => {
  it("Requirement A: merchant@transactx decodes to exact payload (async)", async () => {
    const payload = "merchant@transactx";
    const svg = await encodeQRSvg(payload);
    const decoded = decodeSvgQR(svg);
    assert.equal(decoded, payload, `Decoded '${decoded}' must equal '${payload}'`);
  });

  it("Requirement A: merchant@transactx decodes to exact payload (sync)", () => {
    const payload = "merchant@transactx";
    const svg = encodeQRSvgSync(payload);
    const decoded = decodeSvgQR(svg);
    assert.equal(decoded, payload, `Decoded '${decoded}' must equal '${payload}'`);
  });

  it("Requirement B: different payment identifiers produce different valid QR payloads", async () => {
    const idA = "merchant-a@transactx";
    const idB = "merchant-b@transactx";

    const svgA = await encodeQRSvg(idA);
    const svgB = await encodeQRSvg(idB);

    assert.notEqual(svgA, svgB, "Different identifiers must produce different SVGs");

    const decodedA = decodeSvgQR(svgA);
    const decodedB = decodeSvgQR(svgB);

    assert.equal(decodedA, idA);
    assert.equal(decodedB, idB);
  });

  it("Requirement C: empty and whitespace-only payment identifiers are rejected safely", async () => {
    await assert.rejects(() => encodeQRSvg(""), { message: /must not be empty/i });
    await assert.rejects(() => encodeQRSvg("   "), { message: /must not be empty/i });
    assert.throws(() => encodeQRSvgSync(""), { message: /must not be empty/i });
    assert.throws(() => encodeQRSvgSync("   "), { message: /must not be empty/i });
  });

  it("Requirement D: merchant receive page real encoder generates valid SVG structure", async () => {
    const paymentIdentifier = "store.acme@transactx";
    const svg = await encodeQRSvg(paymentIdentifier);

    assert.ok(svg.includes("<svg"), "Output must be valid SVG");
    assert.ok(svg.includes("viewBox="), "SVG must specify viewBox");
    assert.ok(!svg.includes('width="168"'), "Must NOT use old fake static SVG");

    const decoded = decodeSvgQR(svg);
    assert.equal(decoded, paymentIdentifier);
  });

  it("produces deterministic output for the same payload", async () => {
    const payload = "deterministic@transactx";
    const svg1 = await encodeQRSvg(payload);
    const svg2 = await encodeQRSvg(payload);
    assert.equal(svg1, svg2, "Successive encodes must produce identical SVGs");

    const sync1 = encodeQRSvgSync(payload);
    const sync2 = encodeQRSvgSync(payload);
    assert.equal(sync1, sync2, "Successive sync encodes must produce identical SVGs");
  });

  it("handles variety of valid payment identifier formats", async () => {
    const identifiers = [
      "user@transactx",
      "shop+checkout@transactx",
      "MERCHANT_123@transactx",
      "pay.me-fast@subdomain.example",
      "999888777@transactx",
    ];

    for (const id of identifiers) {
      const svg = await encodeQRSvg(id);
      const decoded = decodeSvgQR(svg);
      assert.equal(decoded, id, `Failed to roundtrip payload: ${id}`);
    }
  });
});
