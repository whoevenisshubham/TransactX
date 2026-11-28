import React from "react";
import type { ConsoleIconName } from "./types";

export function ConsoleIcon({
  name,
  size = 18,
}: {
  name: ConsoleIconName;
  size?: number;
}) {
  const paths: Record<ConsoleIconName, React.ReactNode> = {
    overview: (
      <>
        <rect x="2.5" y="2.5" width="5" height="5" rx="1" />
        <rect x="10.5" y="2.5" width="5" height="5" rx="1" />
        <rect x="2.5" y="10.5" width="5" height="5" rx="1" />
        <rect x="10.5" y="10.5" width="5" height="5" rx="1" />
      </>
    ),
    health: <path d="M2.5 9h3l2-5 2 10 2-5h4" />,
    routing: (
      <>
        <path d="M4 5h5a4 4 0 0 1 4 4v5" />
        <path d="M4 13h5a4 4 0 0 0 4-4V5" />
        <circle cx="4" cy="5" r="1.5" />
        <circle cx="4" cy="13" r="1.5" />
      </>
    ),
    reconciliation: (
      <>
        <rect x="3" y="3.5" width="12" height="4" rx="1" />
        <rect x="3" y="10.5" width="12" height="4" rx="1" />
      </>
    ),
    merkle: (
      <>
        <circle cx="9" cy="4" r="1.5" />
        <circle cx="5" cy="13" r="1.5" />
        <circle cx="13" cy="13" r="1.5" />
        <path d="m9 5.5-4 6M9 5.5l4 6" />
      </>
    ),
    integrity: <path d="M9 2.5 3.5 5v4c0 3.8 5.5 5.5 5.5 5.5s5.5-1.7 5.5-5.5V5L9 2.5Z" />,
    chaos: <path d="m9.5 2-6 8h5l-1 6 6-8h-5l1-6Z" />,
    activity: (
      <>
        <path d="M3 5h12M3 9h12M3 13h12" />
      </>
    ),
    pulse: <path d="M2.5 9h3l2-5 2 10 2-5h4" />,
    refresh: (
      <>
        <path d="M14 7.5A5.5 5.5 0 1 0 14.5 11" />
        <path d="M14.5 5v3h-3" />
      </>
    ),
  };

  return (
    <svg
      aria-hidden="true"
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 18 18"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {paths[name]}
    </svg>
  );
}
