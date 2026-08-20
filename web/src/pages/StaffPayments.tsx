import { useEffect, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type TxnStatus } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, Input, Spinner, formatDateTime, formatFee } from "../components/ui";

type Filter = "all" | "succeeded" | "failed" | "refunds";

// todayISO / shiftISO work in whole calendar days without timezone drift (noon
// UTC anchor), so stepping never skips or repeats a day.
function todayISO(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}
function shiftISO(iso: string, delta: number): string {
  const d = new Date(`${iso}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() + delta);
  return d.toISOString().slice(0, 10);
}
function fmtDay(iso: string): string {
  return new Date(`${iso}T12:00:00Z`).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}
type T = (key: string, opts?: Record<string, unknown>) => string;
function windowLabel(from: string, to: string, t: T): string {
  if (!from && !to) return t("payments.allTime");
  if (from && to) return from === to ? fmtDay(from) : `${fmtDay(from)} – ${fmtDay(to)}`;
  return from ? t("payments.since", { date: fmtDay(from) }) : t("payments.until", { date: fmtDay(to) });
}

const PAGE_SIZES = [10, 25, 50, 100];

export function StaffPayments() {
  const { t } = useTranslation();
  const { isStaff } = useAuth();
  const [filter, setFilter] = useState<Filter>("all");
  // Empty from/to == "all time". Both set == an inclusive day range.
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [pageSize, setPageSize] = useState(25);
  const [page, setPage] = useState(0);

  // Snap back to the first page whenever the visible set changes underneath us.
  useEffect(() => setPage(0), [filter, from, to, pageSize]);
  const { data, isFetching } = useQuery({
    queryKey: ["payments", from, to, filter, page, pageSize],
    queryFn: () => api.payments({ from: from || undefined, to: to || undefined, filter, page, pageSize }),
    enabled: isStaff,
    placeholderData: keepPreviousData, // keep the controls steady while a new page/range loads
  });

  if (!isStaff) return <Card className="p-8 text-center text-slate-500">{t("common.staffOnly")}</Card>;
  if (!data) return <Spinner />;

  const s = data.summary;
  const rows = data.transactions;
  const filters: Filter[] = ["all", "succeeded", "failed", "refunds"];

  const today = todayISO();
  const isAllTime = !from && !to;
  const isToday = from === today && to === today;
  // Step the window by a day. From "all time", stepping starts at today.
  const step = (delta: number) => {
    const base = from || to || today;
    const other = to || from || today;
    setFrom(shiftISO(base, delta));
    setTo(shiftISO(other, delta));
  };

  // Server-driven paging: `total` is the whole matching set; `rows` is this page.
  const total = data.total;
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  const start = page * pageSize;

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-semibold uppercase tracking-wide text-brand-600">{t("payments.eyebrow")}</p>
        <h2 className="text-3xl font-bold tracking-tight">{t("payments.title")}</h2>
        <p className="mt-1 text-slate-500">{t("payments.subtitle")}</p>
      </div>

      {/* Date window controls */}
      <Card className="flex flex-wrap items-end gap-3 p-4">
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => step(-1)}
            aria-label={t("payments.prevDay")}
            className="rounded-lg border border-slate-300 px-2.5 py-2 text-slate-600 hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
          >
            <span aria-hidden="true">◀</span>
          </button>
          <button
            type="button"
            onClick={() => step(1)}
            aria-label={t("payments.nextDay")}
            className="rounded-lg border border-slate-300 px-2.5 py-2 text-slate-600 hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
          >
            <span aria-hidden="true">▶</span>
          </button>
        </div>
        <label className="text-sm">
          <span className="mb-1 block text-slate-500">{t("payments.from")}</span>
          <Input type="date" value={from} max={to || undefined} onChange={(e) => setFrom(e.target.value)} className="w-40" />
        </label>
        <label className="text-sm">
          <span className="mb-1 block text-slate-500">{t("payments.to")}</span>
          <Input type="date" value={to} min={from || undefined} onChange={(e) => setTo(e.target.value)} className="w-40" />
        </label>
        <div className="flex gap-2">
          <Button variant={isToday ? "primary" : "outline"} onClick={() => { setFrom(today); setTo(today); }} aria-pressed={isToday}>
            {t("payments.today")}
          </Button>
          <Button variant={isAllTime ? "primary" : "outline"} onClick={() => { setFrom(""); setTo(""); }} aria-pressed={isAllTime}>
            {t("payments.allTime")}
          </Button>
        </div>
        <span role="status" className="ml-auto self-center text-sm text-slate-500">
          {isFetching ? t("common.loading") : t("payments.showing", { label: windowLabel(from, to, t) })}
        </span>
      </Card>

      {/* Ledger totals */}
      <div className="grid gap-px overflow-hidden rounded-xl border border-slate-200 bg-slate-200 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label={t("payments.collected")} value={formatFee(s.collectedCents)} note={t("payments.collectedNote", { count: s.succeeded })} />
        <StatTile label={t("payments.refunded")} value={formatFee(s.refundedCents)} note={t("payments.refundedNote", { count: s.refunds })} />
        <StatTile label={t("payments.net")} value={formatFee(s.netCents)} note={t("payments.netNote")} />
        <StatTile label={t("payments.failures")} value={String(s.failed)} note={t("payments.failuresNote")} danger={s.failed > 0} />
      </div>

      {/* Filter row */}
      <div role="group" aria-label={t("payments.filter")} className="flex flex-wrap gap-2">
        {filters.map((f) => (
          <button
            key={f}
            type="button"
            onClick={() => setFilter(f)}
            aria-pressed={filter === f}
            className={`rounded-lg border px-3 py-1.5 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 ${filter === f ? "border-brand-500 bg-brand-50 text-brand-700" : "border-slate-300 text-slate-600 hover:bg-slate-50"}`}
          >
            {t(`payments.f_${f}`)}
          </button>
        ))}
      </div>

      {!rows.length ? (
        <Card className="p-8 text-center text-slate-500">{t("payments.empty")}</Card>
      ) : (
        <Card className="overflow-x-auto p-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-100 text-left text-slate-500">
                <th className="px-4 py-3 font-medium">{t("payments.when")}</th>
                <th className="px-4 py-3 font-medium">{t("payments.facility")}</th>
                <th className="px-4 py-3 font-medium">{t("payments.payer")}</th>
                <th className="px-4 py-3 font-medium">{t("payments.type")}</th>
                <th className="px-4 py-3 font-medium">{t("payments.status")}</th>
                <th className="px-4 py-3 text-right font-medium">{t("payments.amount")}</th>
                <th className="px-4 py-3 font-medium">{t("payments.card")}</th>
                <th className="px-4 py-3 font-medium">{t("payments.reference")}</th>
                <th className="px-4 py-3 font-medium">{t("payments.message")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((tx) => (
                <tr key={tx.id} className="border-b border-slate-50 align-top last:border-0">
                  <td className="whitespace-nowrap px-4 py-3 text-slate-600">{formatDateTime(tx.createdAt)}</td>
                  <td className="px-4 py-3 text-slate-700">{tx.facilityName || "—"}</td>
                  <td className="px-4 py-3 text-slate-600">
                    <span className="block">{tx.userName || "—"}</span>
                    <span className="block text-xs text-slate-400">{tx.userEmail}</span>
                  </td>
                  <td className="px-4 py-3">
                    <Badge tone={tx.kind === "refund" ? "slate" : "brand"}>{t(`payments.kind_${tx.kind}`)}</Badge>
                  </td>
                  <td className="px-4 py-3"><StatusBadge status={tx.status} /></td>
                  <td className={`whitespace-nowrap px-4 py-3 text-right font-medium tabular-nums ${tx.kind === "refund" ? "text-slate-500" : "text-slate-800"}`}>
                    {tx.kind === "refund" ? "−" : ""}{formatFee(tx.amountCents)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-slate-500">{tx.cardLast4 ? `•••• ${tx.cardLast4}` : "—"}</td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-500">{tx.providerRef || "—"}</td>
                  <td className="px-4 py-3 text-slate-600">{tx.message || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {rows.length > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-3 text-sm text-slate-600">
          <label className="flex items-center gap-2">
            <span>{t("payments.rowsPerPage")}</span>
            <select
              value={pageSize}
              onChange={(e) => setPageSize(Number(e.target.value))}
              className="rounded-lg border border-slate-300 px-2 py-1.5 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
            >
              {PAGE_SIZES.map((n) => (
                <option key={n} value={n}>{n}</option>
              ))}
            </select>
          </label>

          <div className="flex items-center gap-3">
            <span role="status" className="tabular-nums">
              {t("payments.pageRange", { from: start + 1, to: start + rows.length, total })}
            </span>
            <div className="flex gap-1">
              <button
                type="button"
                onClick={() => setPage(page - 1)}
                disabled={page === 0 || isFetching}
                aria-label={t("payments.prevPage")}
                className="rounded-lg border border-slate-300 px-2.5 py-1.5 text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
              >
                <span aria-hidden="true">◀</span>
              </button>
              <button
                type="button"
                onClick={() => setPage(page + 1)}
                disabled={page >= pageCount - 1 || isFetching}
                aria-label={t("payments.nextPage")}
                className="rounded-lg border border-slate-300 px-2.5 py-1.5 text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
              >
                <span aria-hidden="true">▶</span>
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function StatTile({ label, value, note, danger }: { label: string; value: string; note: string; danger?: boolean }) {
  return (
    <div className="bg-white p-5">
      <p className="text-xs font-semibold uppercase tracking-wide text-brand-600">{label}</p>
      <p className={`mt-1 text-3xl font-bold tracking-tight ${danger ? "text-red-600" : "text-slate-900"}`}>{value}</p>
      <p className="mt-1 text-sm text-slate-500">{note}</p>
    </div>
  );
}

// StatusBadge pairs colour with an icon + label so status never reads by colour
// alone (WCAG).
function StatusBadge({ status }: { status: TxnStatus }) {
  const { t } = useTranslation();
  if (status === "failed") {
    return <Badge tone="red"><span aria-hidden="true">✕ </span>{t("payments.st_failed")}</Badge>;
  }
  return <Badge tone="green"><span aria-hidden="true">✓ </span>{t("payments.st_succeeded")}</Badge>;
}
