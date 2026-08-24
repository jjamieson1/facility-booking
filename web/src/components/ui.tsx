// A small shadcn-style UI kit for the demo.
import { useState } from "react";
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from "react";
import { useTranslation } from "react-i18next";

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`rounded-xl border border-slate-200 bg-white shadow-sm ${className}`}>{children}</div>;
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "outline" | "ghost" | "danger" };

export function Button({ children, className = "", variant = "primary", ...props }: ButtonProps) {
  const styles: Record<string, string> = {
    primary: "bg-brand-500 text-white hover:bg-brand-600",
    outline: "border border-slate-300 bg-white text-slate-700 hover:bg-slate-50",
    ghost: "text-slate-600 hover:bg-slate-100",
    danger: "bg-red-600 text-white hover:bg-red-700",
  };
  return (
    <button
      className={`inline-flex items-center justify-center rounded-lg px-4 py-2 text-sm font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 ${styles[variant]} ${className}`}
      {...props}
    >
      {children}
    </button>
  );
}

export function Input({ className = "", ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={`w-full rounded-lg border border-slate-300 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500 ${className}`}
      {...props}
    />
  );
}

export function Badge({ children, tone = "brand" }: { children: ReactNode; tone?: "brand" | "green" | "amber" | "red" | "slate" }) {
  const tones: Record<string, string> = {
    brand: "bg-brand-50 text-brand-700",
    green: "bg-green-50 text-green-700",
    amber: "bg-amber-50 text-amber-700",
    red: "bg-red-50 text-red-700",
    slate: "bg-slate-100 text-slate-600",
  };
  return <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${tones[tone]}`}>{children}</span>;
}

export function Spinner({ label = "Loading…" }: { label?: string }) {
  return <p role="status" className="text-sm text-slate-500">{label}</p>;
}

export function formatFee(cents: number): string {
  return cents === 0 ? "Free" : `$${(cents / 100).toFixed(2)}`;
}

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    weekday: "short", month: "short", day: "numeric", hour: "numeric", minute: "2-digit",
  });
}

export function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

const statusTone: Record<string, "green" | "amber" | "red" | "slate"> = {
  confirmed: "green", pending: "amber", denied: "red", cancelled: "slate",
};

export function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation();
  return <Badge tone={statusTone[status] ?? "slate"}>{t(`status.${status}`, status)}</Badge>;
}

// FacilityImage renders a facility photo, or a labelled placeholder when there
// is none or the file fails to load.
//
// Two failures it prevents. An empty `src` is not "no image" to a browser — it
// resolves to the current page URL, so the page re-requests itself and renders a
// broken-image icon. And an off-site photo that 404s or is blocked leaves the
// same icon, which on a municipal site reads as a broken site rather than a
// missing photo.
//
// The placeholder is decorative, so it carries aria-hidden and empty alt: the
// facility name is already the adjacent heading, and announcing it twice adds
// nothing for a screen-reader user.
// resolveImage turns whatever is stored on the facility into a URL the browser
// can fetch. Locally-hosted photos are stored as a path relative to the SPA
// ("facilities/ice-arena.jpg") rather than an absolute one, because production
// serves the app under /facility-booking/ — a leading slash would 404 there.
// Absolute URLs are left alone so a municipality can still point at its own CDN.
function resolveImage(src?: string): string {
  const value = (src ?? "").trim();
  if (value === "" || /^(https?:)?\/\//.test(value) || value.startsWith("data:")) return value;
  return `${import.meta.env.BASE_URL}${value.replace(/^\//, "")}`;
}

export function FacilityImage({
  src,
  alt,
  className = "",
}: {
  src?: string;
  alt: string;
  className?: string;
}) {
  const [failed, setFailed] = useState(false);
  const resolved = resolveImage(src);
  const usable = resolved !== "" && !failed;

  if (!usable) {
    return (
      <div
        aria-hidden="true"
        className={`grid place-items-center bg-slate-100 text-slate-400 ${className}`}
      >
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
          <rect x="3" y="5" width="18" height="14" rx="2" />
          <circle cx="8.5" cy="10.5" r="1.5" />
          <path d="M21 15l-5-5-4 4-2-2-4 4" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </div>
    );
  }
  return (
    <img
      src={resolved}
      alt={alt}
      loading="lazy"
      onError={() => setFailed(true)}
      className={className}
    />
  );
}
