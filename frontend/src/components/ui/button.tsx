import React, { forwardRef } from "react";
import { Slot } from "@radix-ui/react-slot";
import { Loader2 } from "lucide-react";

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "accent" | "outline" | "ghost" | "destructive";
  size?: "sm" | "md" | "lg" | "icon";
  asChild?: boolean;
  loading?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  (
    {
      className = "",
      variant = "primary",
      size = "md",
      asChild = false,
      loading = false,
      disabled,
      children,
      ...props
    },
    ref
  ) => {
    const Comp = asChild ? Slot : "button";

    const baseStyles =
      "inline-flex items-center justify-center font-medium rounded-lg transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] focus-visible:ring-offset-2 disabled:opacity-50 disabled:pointer-events-none cursor-pointer";

    const variants: Record<string, string> = {
      primary:
        "bg-[var(--action-primary-bg)] text-[var(--action-primary-fg)] hover:opacity-90 active:scale-[0.98]",
      secondary:
        "bg-[var(--action-secondary-bg)] text-[var(--action-secondary-fg)] border border-[var(--border-hairline)] hover:bg-[var(--action-secondary-hover)] active:scale-[0.98]",
      accent:
        "bg-[var(--action-accent-bg)] text-[var(--action-accent-fg)] hover:opacity-90 active:scale-[0.98]",
      outline:
        "bg-transparent text-[var(--text-primary)] border border-[var(--border-hairline)] hover:bg-[var(--bg-surface-raised)]",
      ghost:
        "bg-transparent text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)]",
      destructive:
        "bg-[var(--status-danger-bg)] text-[var(--status-danger-fg)] border border-[var(--status-danger-border)] hover:opacity-90",
    };

    const sizes: Record<string, string> = {
      sm: "h-8 px-3 text-xs gap-1.5",
      md: "h-10 px-4 text-sm gap-2",
      lg: "h-12 px-6 text-base gap-2.5",
      icon: "h-10 w-10 p-0",
    };

    const isDisabled = disabled || loading;
    const handleClick = (e: React.MouseEvent<HTMLButtonElement>) => {
      if (isDisabled) {
        e.preventDefault();
        return;
      }
      props.onClick?.(e);
    };

    if (asChild) {
      return (
        <Comp
          ref={ref}
          aria-disabled={isDisabled}
          className={`${baseStyles} ${variants[variant]} ${sizes[size]} ${className} ${isDisabled ? "pointer-events-none opacity-50" : ""}`}
          {...props}
          onClick={handleClick}
        >
          {children}
        </Comp>
      );
    }

    return (
      <Comp
        ref={ref}
        disabled={isDisabled}
        className={`${baseStyles} ${variants[variant]} ${sizes[size]} ${className}`}
        {...props}
        onClick={handleClick}
      >
        {loading ? (
          <>
            <Loader2 className="w-4 h-4 animate-spin" />
            <span>Loading...</span>
          </>
        ) : (
          children
        )}
      </Comp>
    );
  }
);

Button.displayName = "Button";
