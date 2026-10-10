import type { ReactNode } from "react";
import { StatusBadge } from "../components";

export function formatPaiseDecimal(value: string, currency = "INR"): string {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) throw new Error("Invalid paise amount");
  const amount = BigInt(value);
  const whole = (amount / 100n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const fraction = (amount % 100n).toString().padStart(2, "0");
  return `${currency === "INR" ? "₹" : `${currency} `}${whole}.${fraction}`;
}

export function OpsStatusBadge({ state }: { state: string }) {
  return <StatusBadge state={state} />;
}

export type TableColumn<T> = { key: string; heading: string; render: (item: T) => ReactNode };
export function DataTable<T>({ caption, columns, rows, rowKey }: {
  caption: string; columns: TableColumn<T>[]; rows: T[]; rowKey: (item: T) => string;
}) {
  return <div className="ops-table-scroll"><table className="ops-table">
    <caption>{caption}</caption>
    <thead><tr>{columns.map((column) => <th key={column.key} scope="col">{column.heading}</th>)}</tr></thead>
    <tbody>{rows.map((row) => <tr key={rowKey(row)}>{columns.map((column) => <td key={column.key} data-label={column.heading}>{column.render(row)}</td>)}</tr>)}</tbody>
  </table></div>;
}

export function Timeline<T>({ title, entries, entryKey, when, heading, detail }: {
  title: string; entries: T[]; entryKey: (entry: T) => string;
  when: (entry: T) => string; heading: (entry: T) => ReactNode; detail?: (entry: T) => ReactNode;
}) {
  return <section className="ops-timeline" aria-label={title}>
    <h2>{title}</h2>
    <ol>{entries.map((entry) => <li key={entryKey(entry)}>
      <time dateTime={when(entry)}>{new Date(when(entry)).toLocaleString()}</time>
      <strong>{heading(entry)}</strong>
      {detail?.(entry)}
    </li>)}</ol>
  </section>;
}
