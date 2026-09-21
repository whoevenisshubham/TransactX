/**
 * qr.ts — Standards-compliant QR code generation.
 *
 * Uses the `qrcode` npm package to produce ISO 18004-compliant QR codes.
 * The output is a valid SVG string that any standard QR scanner can decode.
 */
import QRCode from "qrcode";

export type QROptions = {
  /** Colour of dark modules (default: #17201d, the project ink colour). */
  darkColor?: string;
  /** Colour of light modules / background (default: #ffffff). */
  lightColor?: string;
  /** Error-correction level: L (7%), M (15%), Q (25%), H (30%). */
  errorCorrectionLevel?: "L" | "M" | "Q" | "H";
  /** Margin in modules (default: 4 for ISO compliance quiet zone). */
  margin?: number;
};

/**
 * Encode `payload` as a standards-compliant QR code SVG string.
 *
 * The returned SVG can be embedded via dangerouslySetInnerHTML or injected
 * into the DOM. Any conforming QR-code scanner can decode it.
 *
 * @throws {Error} if `payload` is empty or whitespace-only.
 */
export async function encodeQRSvg(payload: string, opts?: QROptions): Promise<string> {
  if (!payload || payload.trim() === "") {
    throw new Error("QR payload must not be empty");
  }
  return QRCode.toString(payload, {
    type: "svg",
    color: {
      dark: opts?.darkColor ?? "#17201d",
      light: opts?.lightColor ?? "#ffffff",
    },
    errorCorrectionLevel: opts?.errorCorrectionLevel ?? "M",
    margin: opts?.margin ?? 4,
  });
}

/**
 * Synchronous version using the ISO 18004 QR bit matrix generator.
 * Useful in synchronous tests and deterministic rendering.
 *
 * @throws {Error} if `payload` is empty or whitespace-only.
 */
export function encodeQRSvgSync(payload: string, opts?: QROptions): string {
  if (!payload || payload.trim() === "") {
    throw new Error("QR payload must not be empty");
  }
  const qr = QRCode.create(payload, {
    errorCorrectionLevel: opts?.errorCorrectionLevel ?? "M",
  });
  return renderQRMatrixToSvg(
    qr.modules,
    opts?.darkColor ?? "#17201d",
    opts?.lightColor ?? "#ffffff",
    opts?.margin ?? 4,
  );
}

/**
 * Render a QR bit-matrix to a valid SVG string.
 */
export function renderQRMatrixToSvg(
  modules: { size: number; data: Uint8Array | Uint8ClampedArray | number[] },
  dark: string,
  light: string,
  margin = 4,
): string {
  const size = modules.size;
  const total = size + margin * 2;
  let paths = "";
  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      if (modules.data[r * size + c]) {
        paths += `<rect x="${margin + c}" y="${margin + r}" width="1" height="1"/>`;
      }
    }
  }
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${total} ${total}" shape-rendering="crispEdges"><rect width="${total}" height="${total}" fill="${light}"/><g fill="${dark}">${paths}</g></svg>`;
}
