import React, { forwardRef } from "react";

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  error?: boolean;
  startAdornment?: React.ReactNode;
  endAdornment?: React.ReactNode;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ className = "", error, startAdornment, endAdornment, disabled, ...props }, ref) => {
    return (
      <div
        className={`relative flex items-center w-full rounded-lg border transition-all ${
          error
            ? "border-[var(--status-danger-fg)] ring-1 ring-[var(--status-danger-fg)]"
            : "border-[var(--border-hairline)] focus-within:border-[var(--border-focus)] focus-within:ring-2 focus-within:ring-[var(--border-focus)]/20"
        } bg-[var(--bg-surface)] ${
          disabled ? "opacity-60 bg-[var(--bg-surface-raised)] cursor-not-allowed" : ""
        } ${className}`}
      >
        {startAdornment && (
          <div className="flex items-center pl-3 text-[var(--text-secondary)] select-none">
            {startAdornment}
          </div>
        )}
        <input
          ref={ref}
          disabled={disabled}
          className="w-full h-10 px-3 py-2 text-sm bg-transparent text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-none disabled:cursor-not-allowed"
          {...props}
        />
        {endAdornment && (
          <div className="flex items-center pr-3 text-[var(--text-secondary)] select-none">
            {endAdornment}
          </div>
        )}
      </div>
    );
  }
);

Input.displayName = "Input";
