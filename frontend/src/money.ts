/** Exact INR amount parsing for customer payments. Uses BigInt only — never Number/parseFloat/Math.round for money. */
export function parsePaise(input: string): number | null {
  const value = input.trim();
  if (!/^(0|[1-9]\d*)(?:\.(\d{1,2}))?$/.test(value)) return null;
  const [whole, fraction = ""] = value.split(".");
  const paise = BigInt(whole) * 100n + BigInt((fraction + "00").slice(0, 2));
  if (paise <= 0n || paise > BigInt(Number.MAX_SAFE_INTEGER)) return null;
  return Number(paise);
}
