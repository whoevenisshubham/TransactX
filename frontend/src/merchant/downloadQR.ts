/** Download the actual local encoder output; no external image/QR service. */
export async function downloadQR(svg: string, format: "svg" | "png") {
  const qrDocument = new DOMParser().parseFromString(svg, "image/svg+xml");
  qrDocument.documentElement.setAttribute("width", "768");
  qrDocument.documentElement.setAttribute("height", "768");
  const svgBlob = new Blob([new XMLSerializer().serializeToString(qrDocument)], { type: "image/svg+xml" });
  let result: Blob = svgBlob;
  if (format === "png") {
    const source = URL.createObjectURL(svgBlob);
    try {
      const image = new Image();
      image.src = source;
      await image.decode();
      const canvas = document.createElement("canvas");
      canvas.width = canvas.height = 768;
      const context = canvas.getContext("2d");
      if (!context) throw new Error("Image export is unavailable in this browser.");
      context.fillStyle = "#fff";
      context.fillRect(0, 0, 768, 768);
      context.drawImage(image, 0, 0, 768, 768);
      result = await new Promise<Blob>((resolve, reject) => canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error("PNG export failed.")), "image/png"));
    } finally { URL.revokeObjectURL(source); }
  }
  const url = URL.createObjectURL(result);
  const link = document.createElement("a");
  link.href = url; link.download = `transactx-receive.${format}`;
  document.body.append(link); link.click(); link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}
