"use client";

import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "@/lib/api";
import { appointmentStatusLabels, roleLabels, type Appointment, type Role } from "@/lib/types";

/* ─── Page heading ───────────────────────────────────────────────────────── */
export function PageHeading({ eyebrow, title, description, action }: { eyebrow?: string; title: string; description?: string; action?: ReactNode }) {
  return (
    <header className="page-heading">
      <div className="page-heading-text">
        {eyebrow && <span className="eyebrow">{eyebrow}</span>}
        <h1>{title}</h1>
        {description && <p className="muted">{description}</p>}
      </div>
      {action && <div className="page-heading-actions">{action}</div>}
    </header>
  );
}

// Kept as an alias while individual pages progressively use the more explicit name.
export const PageHeader = PageHeading;

/* ─── Skeleton loading ───────────────────────────────────────────────────── */
export function SkeletonRow({ cols = 4 }: { cols?: number }) {
  const widths = ["45%", "30%", "20%", "10%", "25%", "35%"];
  return (
    <tr aria-hidden="true">
      {Array.from({ length: cols }, (_, i) => (
        <td key={i}>
          <span
            className="skeleton-block"
            style={{ height: "14px", width: widths[i % widths.length], display: "block", borderRadius: "4px" }}
          />
        </td>
      ))}
    </tr>
  );
}

export function SkeletonRows({ cols = 4, rows = 5 }: { cols?: number; rows?: number }) {
  return (
    <>
      {Array.from({ length: rows }, (_, i) => <SkeletonRow key={i} cols={cols} />)}
    </>
  );
}

export function LoadingState({ label }: { label?: string }) {
  return (
    <div className="panel loading-panel" aria-live="polite" aria-busy="true">
      <div style={{ padding: "1.25rem 0" }}>
        {[70, 50, 85, 45, 60].map((w, i) => (
          <div key={i} className="skeleton-row" style={{ border: "none" }}>
            <span className="skeleton-block" style={{ height: "13px", width: `${w}%`, borderRadius: "4px" }} />
          </div>
        ))}
      </div>
      {label && <p className="muted" style={{ textAlign: "center", fontSize: ".8125rem" }}>{label}</p>}
    </div>
  );
}

/* ─── Empty state ────────────────────────────────────────────────────────── */
export function EmptyState({ title, body, action }: { title: string; body: string; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <h2>{title}</h2>
      <p>{body}</p>
      {action}
    </div>
  );
}

/* ─── Error state ────────────────────────────────────────────────────────── */
export function ErrorNotice({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const message = error instanceof ApiError ? error.message : "Bu veriler yüklenemedi.";
  return (
    <div className="notice error" role="alert" style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "1rem" }}>
      <span>{message}</span>
      {onRetry && (
        <button className="button danger sm" onClick={onRetry} type="button">Tekrar dene</button>
      )}
    </div>
  );
}

/* ─── Status badge ───────────────────────────────────────────────────────── */
const STATUS_CLASS: Record<string, string> = {
  active: "active",
  inactive: "inactive",
  pending: "pending",
  confirmed: "confirmed",
  cancelled: "cancelled",
  completed: "completed",
  no_show: "no-show",
};

const ROLE_CLASS: Record<string, string> = {
  OWNER: "role-owner",
  MANAGER: "role-manager",
  BARBER: "role-barber",
  RECEPTIONIST: "role-receptionist",
};

export function StatusBadge({ active, label }: { active?: boolean; label?: string }) {
  const statusKey = label ? label.replace(/ /g, "_").toLowerCase() : (active ? "active" : "inactive");
  const cls = STATUS_CLASS[statusKey] ?? (active ? "active" : "inactive");
  const text = statusKey in appointmentStatusLabels
    ? appointmentStatusLabels[statusKey as Appointment["status"]]
    : (label ?? (active ? "Aktif" : "Pasif"));
  return <span className={`badge ${cls}`}>{text.replace(/_/g, " ")}</span>;
}

export function StatusIndicator({ active, label }: { active?: boolean; label?: string }) {
  const statusKey = label ? label.replace(/ /g, "_").toLowerCase() : (active ? "active" : "inactive");
  const cls = STATUS_CLASS[statusKey] ?? (active ? "active" : "inactive");
  const text = statusKey in appointmentStatusLabels
    ? appointmentStatusLabels[statusKey as Appointment["status"]]
    : (label ?? (active ? "Aktif" : "Pasif"));
  return <span className={`status-indicator ${cls}`}><i aria-hidden="true" />{text.replace(/_/g, " ")}</span>;
}

export function RoleBadge({ role }: { role: string }) {
  const cls = ROLE_CLASS[role] ?? "role-owner";
  return <span className={`badge ${cls}`}>{roleLabels[role as Role] ?? role}</span>;
}

/* ─── Form field ─────────────────────────────────────────────────────────── */
export function FormField({
  label, required, help, error, id, children,
}: {
  label: string; required?: boolean; help?: string; error?: string; id?: string; children: ReactNode;
}) {
  return (
    <div className="form-field">
      <label htmlFor={id} className="form-field-label">
        {label}
        {required && <span className="required" aria-hidden="true"> *</span>}
      </label>
      {children}
      {help && !error && <span className="form-field-help">{help}</span>}
      {error && <span className="form-field-error" role="alert">{error}</span>}
    </div>
  );
}

export function FormError({ value }: { value?: string }) {
  return value ? <p className="form-error" role="alert">{value}</p> : null;
}

/* ─── Modal ──────────────────────────────────────────────────────────────── */
export function Modal({ title, children, onClose, wide = false }: { title: string; children: ReactNode; onClose: () => void; wide?: boolean }) {
  return <Drawer title={title} onClose={onClose} wide={wide}>{children}</Drawer>;
}

/* ─── ConfirmDialog ──────────────────────────────────────────────────────── */
export function ConfirmDialog({
  title, description, confirmLabel, busy = false, variant = "danger", onCancel, onConfirm,
}: {
  title: string; description: string; confirmLabel: string; busy?: boolean; variant?: "danger" | "warning"; onCancel: () => void; onConfirm: () => void;
}) {
  return (
    <div className="confirm-layer" role="presentation">
      <button aria-label="Onay penceresini kapat" className="confirm-backdrop" onClick={onCancel} type="button" />
      <section aria-modal="true" aria-labelledby="confirm-title" className="confirm-dialog" role="dialog">
        <span className="eyebrow">İşlemi onaylayın</span>
        <h2 id="confirm-title">{title}</h2>
        <p className="muted">{description}</p>
        <div className="dialog-actions">
          <button className="button secondary" disabled={busy} onClick={onCancel} type="button">Vazgeç</button>
          <button className={`button ${variant}`} disabled={busy} onClick={onConfirm} type="button">
            {busy ? "İşleniyor…" : confirmLabel}
          </button>
        </div>
      </section>
    </div>
  );
}

/* ─── Drawer ─────────────────────────────────────────────────────────────── */
export function Drawer({
  title, children, footer, onClose, wide = false,
}: {
  title: string; children: ReactNode; footer?: ReactNode; onClose: () => void; wide?: boolean;
}) {
  const closeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    closeRef.current?.focus();
    return () => previous?.focus();
  }, []);

  useEffect(() => {
    function handleKey(e: KeyboardEvent) { if (e.key === "Escape") onClose(); }
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [onClose]);

  return (
    <div className="drawer-layer" role="presentation">
      <button aria-label="Paneli kapat" className="drawer-backdrop" onClick={onClose} type="button" />
      <section aria-modal="true" aria-labelledby="drawer-title" className={`drawer${wide ? " wide" : ""}`} role="dialog">
        <header className="drawer-header">
          <h2 id="drawer-title">{title}</h2>
          <button ref={closeRef} aria-label="Paneli kapat" className="dialog-close" onClick={onClose} type="button">×</button>
        </header>
        <div className="drawer-body">{children}</div>
        {footer && <div className="drawer-footer">{footer}</div>}
      </section>
    </div>
  );
}

export function DataList({ children, label }: { children: ReactNode; label?: string }) {
  return <div className="data-list" aria-label={label}>{children}</div>;
}

export function DataRow({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`data-row ${className}`.trim()}>{children}</div>;
}

export type TimelineItem = { id: string; time: string; title: string; detail?: string; status?: string };

export function Timeline({ items, empty }: { items: TimelineItem[]; empty: ReactNode }) {
  if (items.length === 0) return <>{empty}</>;
  return (
    <div className="timeline">
      {items.map((item) => (
        <div className="timeline-row" key={item.id}>
          <time>{item.time}</time>
          <div className="timeline-line" aria-hidden="true" />
          <div className="timeline-entry">
            <strong>{item.title}</strong>
            {item.detail && <span>{item.detail}</span>}
          </div>
          {item.status && <StatusIndicator label={item.status} />}
        </div>
      ))}
    </div>
  );
}

export type ScheduleGridDay = { label: string; intervals: { start: string; end: string }[] };

export function ScheduleGrid({ days }: { days: ScheduleGridDay[] }) {
  return (
    <div className="schedule-grid" aria-label="Haftalık çalışma saatleri">
      {days.map((day) => (
        <section className="schedule-grid-day" key={day.label}>
          <h3>{day.label}</h3>
          {day.intervals.length === 0 ? <p>Kapalı</p> : (
            <div className="schedule-blocks">
              {day.intervals.map((interval) => <span key={`${interval.start}-${interval.end}`}>{interval.start}<i />{interval.end}</span>)}
            </div>
          )}
        </section>
      ))}
    </div>
  );
}

/* ─── ActionMenu ─────────────────────────────────────────────────────────── */
export type ActionMenuItem = { label: string; onClick: () => void; variant?: "default" | "danger"; disabled?: boolean };

export function ActionMenu({ items }: { items: ActionMenuItem[] }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);

  useEffect(() => {
    if (!open) return;
    function handle(e: MouseEvent) {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) close();
    }
    function handleKey(e: KeyboardEvent) { if (e.key === "Escape") close(); }
    document.addEventListener("mousedown", handle);
    document.addEventListener("keydown", handleKey);
    return () => { document.removeEventListener("mousedown", handle); document.removeEventListener("keydown", handleKey); };
  }, [open, close]);

  return (
    <div className="action-menu-wrap" ref={wrapRef}>
      <button
        className="action-menu-trigger"
        aria-label="İşlemler"
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((v) => !v)}
        type="button"
      >
        ⋯
      </button>
      {open && (
        <div className="action-menu-dropdown" role="menu">
          {items.map((item, i) => (
            <button
              key={i}
              className={`action-menu-item${item.variant === "danger" ? " danger" : ""}`}
              disabled={item.disabled}
              onClick={() => { item.onClick(); close(); }}
              role="menuitem"
              type="button"
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
