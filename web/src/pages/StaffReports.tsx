import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type Dashboard, type Period, type TrendPoint } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Card, Spinner } from "../components/ui";

const money = (cents: number) => `$${Math.round(cents / 100).toLocaleString()}`;

export function StaffReports() {
  const { t } = useTranslation();
  const { isStaff } = useAuth();
  const [period, setPeriod] = useState<Period>("quarter");
  const { data, isLoading } = useQuery({ queryKey: ["report", period], queryFn: () => api.report(period), enabled: isStaff });

  if (!isStaff) return <Card className="p-8 text-center text-slate-500">{t("common.staffOnly")}</Card>;
  if (isLoading || !data) return <Spinner />;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-brand-600">
            {t("reports.reporting")} · {data.periodLabel}
          </p>
          <h2 className="text-3xl font-bold tracking-tight">{t("reports.title")}</h2>
        </div>
        <PeriodToggle period={period} onChange={setPeriod} />
      </div>

      {/* Stat tiles */}
      <div className="grid gap-px overflow-hidden rounded-xl border border-slate-200 bg-slate-200 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label={t("reports.revenue")} value={money(data.revenueCents)} delta={data.revenueDeltaPct} deltaLabel={data.prevLabel} />
        <StatTile label={t("reports.bookings")} value={data.bookings.toLocaleString()} delta={data.bookingsDeltaPct} deltaLabel={data.prevLabel} />
        <StatTile label={t("reports.avgUtil")} value={`${data.avgUtilizationPct}%`} note={t("reports.ofOpenDays")} />
        <StatTile
          label={t("reports.pending")}
          value={String(data.pending)}
          note={data.pendingOver24h > 0 ? t("reports.overHrs", { count: data.pendingOver24h }) : t("reports.allClear")}
        />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <div className="space-y-6">
          <Card className="p-6">
            <h3 className="mb-4 text-sm font-semibold uppercase tracking-wide text-slate-700">{t("reports.byFacility")}</h3>
            <FacilityBars data={data.byFacility} />
          </Card>
          <Card className="p-6">
            <h3 className="mb-4 text-sm font-semibold uppercase tracking-wide text-slate-700">{t("reports.trend")}</h3>
            <TrendChart points={data.trend} />
          </Card>
        </div>

        <div className="space-y-6">
          <Card className="p-6">
            <h3 className="mb-4 text-sm font-semibold uppercase tracking-wide text-slate-700">{t("reports.topSpaces")}</h3>
            <TopSpaces data={data} />
          </Card>
          <Card className="p-6">
            <h3 className="mb-4 text-sm font-semibold uppercase tracking-wide text-slate-700">{t("reports.residentSplit")}</h3>
            <ResidentSplit residentPct={data.residentPct} />
            <p className="mt-5 border-t border-slate-100 pt-4 text-xs text-slate-500">{t("reports.footnote")}</p>
          </Card>
        </div>
      </div>
    </div>
  );
}

function PeriodToggle({ period, onChange }: { period: Period; onChange: (p: Period) => void }) {
  const { t } = useTranslation();
  const opts: Period[] = ["month", "quarter", "year"];
  return (
    <div role="group" aria-label={t("reports.period")} className="flex overflow-hidden rounded-lg border border-slate-300 text-sm">
      {opts.map((p) => (
        <button
          key={p}
          type="button"
          onClick={() => onChange(p)}
          aria-pressed={period === p}
          className={`px-4 py-2 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-500 ${period === p ? "bg-brand-500 text-white" : "bg-white text-slate-600 hover:bg-slate-50"}`}
        >
          {t(`reports.${p}`)}
        </button>
      ))}
    </div>
  );
}

function StatTile({ label, value, delta, deltaLabel, note }: { label: string; value: string; delta?: number; deltaLabel?: string; note?: string }) {
  const up = (delta ?? 0) >= 0;
  return (
    <div className="bg-white p-5">
      <p className="text-xs font-semibold uppercase tracking-wide text-brand-600">{label}</p>
      <p className="mt-1 text-3xl font-bold tracking-tight text-slate-900">{value}</p>
      {delta !== undefined ? (
        <p className={`mt-1 text-sm font-medium ${up ? "text-green-600" : "text-red-600"}`}>
          <span aria-hidden="true">{up ? "▲" : "▼"}</span> {Math.abs(Math.round(delta))}% {deltaLabel}
        </p>
      ) : (
        <p className="mt-1 text-sm text-slate-500">{note}</p>
      )}
    </div>
  );
}

function FacilityBars({ data }: { data: { facilityName: string; bookings: number }[] }) {
  const max = Math.max(1, ...data.map((d) => d.bookings));
  return (
    <div className="space-y-2.5">
      {data.map((d) => (
        <div key={d.facilityName} className="flex items-center gap-3">
          <span className="w-36 shrink-0 truncate text-right text-sm text-slate-600" title={d.facilityName}>{d.facilityName}</span>
          <div className="h-4 flex-1 rounded bg-slate-100">
            <div
              className="h-4 rounded bg-brand-500"
              style={{ width: `${(d.bookings / max) * 100}%` }}
              title={`${d.facilityName}: ${d.bookings}`}
            />
          </div>
          <span className="w-8 shrink-0 text-right text-sm font-medium tabular-nums text-slate-700">{d.bookings}</span>
        </div>
      ))}
    </div>
  );
}

function TrendChart({ points }: { points: TrendPoint[] }) {
  if (!points.length) return <p className="text-sm text-slate-500">—</p>;
  const W = 500, H = 170, padL = 28, padB = 24, padT = 10, padR = 8;
  const max = Math.max(20, ...points.map((p) => p.utilizationPct));
  const x = (i: number) => padL + (i * (W - padL - padR)) / Math.max(1, points.length - 1);
  const y = (v: number) => padT + (1 - v / max) * (H - padT - padB);
  const line = points.map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.utilizationPct).toFixed(1)}`).join(" ");

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="Utilization trend">
      {[0, Math.round(max / 2), max].map((g) => (
        <g key={g}>
          <line x1={padL} x2={W - padR} y1={y(g)} y2={y(g)} className="stroke-slate-100" strokeWidth={1} />
          <text x={0} y={y(g) + 3} className="fill-slate-400 text-[10px]">{g}</text>
        </g>
      ))}
      <path d={line} fill="none" className="stroke-brand-500" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
      {points.map((p, i) => (
        <g key={p.label}>
          <circle cx={x(i)} cy={y(p.utilizationPct)} r={3.5} className="fill-brand-500" />
          <text x={x(i)} y={H - 6} textAnchor="middle" className="fill-slate-500 text-[10px]">{p.label}</text>
        </g>
      ))}
    </svg>
  );
}

function TopSpaces({ data }: { data: Dashboard }) {
  const { t } = useTranslation();
  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-slate-100 text-left text-xs uppercase tracking-wide text-slate-500">
          <th className="pb-2">{t("reports.space")}</th>
          <th className="pb-2 text-right">{t("reports.rev")}</th>
          <th className="pb-2 text-right">{t("reports.util")}</th>
        </tr>
      </thead>
      <tbody>
        {data.topSpaces.map((s) => (
          <tr key={s.facilityName} className="border-b border-slate-50 last:border-0">
            <td className="py-3 font-medium text-slate-800">{s.facilityName}</td>
            <td className="py-3 text-right tabular-nums text-slate-700">{money(s.revenueCents)}</td>
            <td className="py-3 text-right">
              <span className="rounded bg-brand-50 px-2 py-0.5 text-xs font-medium tabular-nums text-brand-700">{s.utilizationPct}%</span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ResidentSplit({ residentPct }: { residentPct: number }) {
  const { t } = useTranslation();
  const nonRes = 100 - residentPct;
  return (
    <div className="flex h-7 w-full overflow-hidden rounded-lg bg-slate-100" role="img" aria-label={`${residentPct}% resident, ${nonRes}% non-resident`}>
      <div className="flex items-center bg-brand-500 pl-2.5 text-xs font-medium text-white" style={{ width: `${residentPct}%` }}>
        {residentPct >= 15 ? `${t("reports.resident")} ${residentPct}%` : ""}
      </div>
      <div className="flex items-center bg-slate-400 pl-2.5 text-xs font-medium text-white" style={{ width: `${nonRes}%` }}>
        {nonRes >= 15 ? `${t("reports.nonResident")} ${nonRes}%` : ""}
      </div>
    </div>
  );
}
