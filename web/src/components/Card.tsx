import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

interface CardProps {
  title?: string;
  value?: string | number;
  subtitle?: string;
  icon?: ReactNode;
  className?: string;
  children?: ReactNode;
}

export function StatCard({ title, value, subtitle, icon, className }: CardProps) {
  return (
    <div className={cn("bg-surface rounded-xl border border-border p-5", className)}>
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm text-text-muted">{title}</span>
        {icon && <span className="text-text-muted">{icon}</span>}
      </div>
      <div className="text-3xl font-bold tracking-tight">{value}</div>
      {subtitle && <div className="text-xs text-text-muted mt-1">{subtitle}</div>}
    </div>
  );
}

export function Card({ className, children }: CardProps) {
  return (
    <div className={cn("bg-surface rounded-xl border border-border p-5", className)}>
      {children}
    </div>
  );
}
