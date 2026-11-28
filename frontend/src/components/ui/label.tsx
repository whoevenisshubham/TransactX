import React, { forwardRef } from "react";

export interface LabelProps extends React.LabelHTMLAttributes<HTMLLabelElement> {
  required?: boolean;
}

export const Label = forwardRef<HTMLLabelElement, LabelProps>(
  ({ className = "", required, children, ...props }, ref) => {
    return (
      <label
        ref={ref}
        className={`block text-xs font-medium text-[var(--text-secondary)] mb-1.5 select-none ${className}`}
        {...props}
      >
        {children}
        {required && <span className="text-[var(--status-danger-fg)] ml-1">*</span>}
      </label>
    );
  }
);

Label.displayName = "Label";
