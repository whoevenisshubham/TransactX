import { useState } from "react";
import type { AnalyticsWindow } from "./contracts";

type Series = { key: string; title: string; duration: number; items: AnalyticsWindow[] };

export function analyticsSeries(items: AnalyticsWindow[]): Series[] {
  const groups = new Map<string, Series>();
  for (const item of items) {
    const duration = Date.parse(item.to) - Date.parse(item.from);
    if (!Number.isFinite(duration) || duration <= 0) continue;
    const key = JSON.stringify([item.entityType, item.entityId, duration, item.featureSchemaVersion]);
    let group = groups.get(key);
    if (!group) {
      group = { key, duration, title: `${item.entityType.replaceAll("_", " ")} ${item.entityId} · ${duration / 60000} min · ${item.featureSchemaVersion}`, items: [] };
      groups.set(key, group);
    }
    group.items.push(item);
  }
  return [...groups.values()].map(group => ({ ...group, items: [...group.items].sort((left, right) => Date.parse(left.from) - Date.parse(right.from)) }));
}

export function AnalyticsCharts({ items }: { items: AnalyticsWindow[] }) {
  const groups = analyticsSeries(items);
  const [selected, setSelected] = useState("");
  const group = groups.find(group => group.key === selected) ?? groups[0];
  if (!group) return <p className="ops-muted">No valid observation intervals can be plotted. Raw observations remain in the table.</p>;
  return <section className="analytics-visuals" aria-label="Window charts"><div className="analytics-series-heading"><div><p className="eyebrow">Measured windows · UTC</p><h2>See the shape of the observations.</h2></div><label>Entity and window<select aria-label="Entity and window" value={group.key} onChange={event => setSelected(event.target.value)}>{groups.map(series => <option key={series.key} value={series.key}>{series.title}</option>)}</select></label></div><p className="ops-muted analytics-scope">{group.items.length} windows for this entity, duration and feature schema. Gaps are unobserved time; missing measurements are not zero. The table below retains all groups on this page.</p><div className="analytics-chart-grid"><WindowChart group={group} metric="rates" /><WindowChart group={group} metric="latency" /></div></section>;
}

function WindowChart({ group, metric }: { group: Series; metric: "rates" | "latency" }) {
  const from = Math.min(...group.items.map(item => Date.parse(item.from)));
  const to = Math.max(...group.items.map(item => Date.parse(item.to)));
  const span = to - from;
  const chartWidth = 516;
  const scaleX = (time: number) => 48 + (time - from) / span * chartWidth;
  const maxLatency = Math.max(1, ...group.items.map(item => item.features.p95LatencyMs ?? 0));
  const maximum = metric === "rates" ? 1 : maxLatency;
  const title = metric === "rates" ? "Timeout and failure rate" : "P95 latency";
  const noObservations = group.items.every(item => metric === "rates" ? item.features.timeoutRate === null && item.features.failureRate === null : item.features.p95LatencyMs === null);
  return <figure className="analytics-chart"><figcaption>{title}<span>{metric === "rates" ? "Percent of observations" : "Milliseconds per window"}</span></figcaption>{noObservations ? <p className="ops-muted">No measured {metric === "rates" ? "rate" : "latency"} is available in these windows.</p> : <div className="analytics-chart-scroll" role="region" tabIndex={0} aria-label={`${title} chart; scroll horizontally if needed`}><svg viewBox="0 0 600 210" role="img" aria-label={`${title}, ${group.title}. Actual observation intervals in UTC. Dashed markers mean unavailable; see the measured-windows table for values.`}>
    {[0, .5, 1].map(fraction => <g key={fraction}><line x1={48} x2={564} y1={150 - fraction * 112} y2={150 - fraction * 112} className="chart-grid-line" /><text x={40} y={154 - fraction * 112} textAnchor="end" className="chart-axis-label">{metric === "rates" ? `${fraction * 100}%` : Math.round(fraction * maximum)}</text></g>)}
    {group.items.map(item => {
      const start = scaleX(Date.parse(item.from));
      const width = Math.max(1, scaleX(Date.parse(item.to)) - start - 2);
      const values = metric === "rates" ? [item.features.timeoutRate, item.features.failureRate] : [item.features.p95LatencyMs];
      return <g key={item.id}><rect x={start} y={156} width={width} height={3} className="chart-window-span" />{values.map((value, index) => value === null ? <line key={index} x1={start + width / 2} x2={start + width / 2} y1={54} y2={138} className="chart-missing-marker"><title>{new Date(item.from).toISOString()} · {index === 1 ? "Failure" : metric === "rates" ? "Timeout" : "Latency"}: unavailable</title></line> : <rect key={index} x={start + width / values.length * index + 1} y={150 - value / maximum * 112} width={Math.max(1, width / values.length - 2)} height={Math.max(value === 0 ? 1 : 0, value / maximum * 112)} className={index === 1 ? "chart-bar-secondary" : "chart-bar-primary"}><title>{new Date(item.from).toISOString()} · {metric === "rates" ? `${index === 1 ? "Failure" : "Timeout"}: ${(value * 100).toFixed(1)}%` : `P95: ${value.toFixed(1)} ms`} · {item.sampleCount} samples</title></rect>)}</g>;
    })}
    <text x={48} y={185} className="chart-axis-label">{new Date(from).toISOString().slice(5, 16).replace("T", " ")} UTC</text><text x={564} y={185} textAnchor="end" className="chart-axis-label">{new Date(to).toISOString().slice(5, 16).replace("T", " ")} UTC</text>
  </svg></div>}<div className="chart-legend"><span><i className="chart-swatch-primary" />{metric === "rates" ? "Timeout" : "Latency"}</span>{metric === "rates" && <span><i className="chart-swatch-secondary" />Failure</span>}<span>Dashed = unavailable</span></div></figure>;
}
