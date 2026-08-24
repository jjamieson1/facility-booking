import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { api, type CalendarDay, type FacilityCalendar, type SlotStatus } from "../lib/api";
import { Card, Spinner } from "../components/ui";

// --- date helpers (YYYY-MM-DD string math, local) --------------------------
function mondayStr(d = new Date()): string {
  const x = new Date(d);
  x.setDate(x.getDate() - ((x.getDay() + 6) % 7)); // back up to Monday
  return x.toISOString().slice(0, 10);
}
function addDays(iso: string, n: number): string {
  const d = new Date(iso + "T00:00:00");
  d.setDate(d.getDate() + n);
  return d.toISOString().slice(0, 10);
}
function hourLabel(minute: number): string {
  const h = Math.floor(minute / 60);
  const period = h < 12 ? "AM" : "PM";
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return `${h12} ${period}`;
}
function longDate(iso: string): string {
  return new Date(iso + "T00:00:00").toLocaleDateString(undefined, { day: "numeric", month: "long", year: "numeric" });
}
function monthLabel(iso: string): string {
  return new Date(iso + "T00:00:00").toLocaleDateString(undefined, { month: "long", year: "numeric" });
}
function firstMondayOfMonth(iso: string): string {
  const d = new Date(iso + "T00:00:00");
  return mondayStr(new Date(d.getFullYear(), d.getMonth(), 1));
}
function addMonths(iso: string, n: number): string {
  const d = new Date(iso + "T00:00:00");
  return new Date(d.getFullYear(), d.getMonth() + n, 1).toISOString().slice(0, 10);
}

export function FacilityCalendar() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [weekStart, setWeekStart] = useState(mondayStr());
  const [view, setView] = useState<"week" | "month">("week");

  const { data: facilities } = useQuery({ queryKey: ["facilities", {}], queryFn: () => api.listFacilities() });
  const facilityId = params.get("facility") || facilities?.[0]?.id || "";

  const from = view === "week" ? weekStart : firstMondayOfMonth(weekStart);
  const days = view === "week" ? 7 : 42;
  const { data: cal, isLoading } = useQuery({
    queryKey: ["calendar", facilityId, from, days],
    queryFn: () => api.facilityCalendar(facilityId, from, days),
    enabled: !!facilityId,
  });

  const goPrev = () => setWeekStart((w) => (view === "week" ? addDays(w, -7) : addMonths(w, -1)));
  const goNext = () => setWeekStart((w) => (view === "week" ? addDays(w, 7) : addMonths(w, 1)));
  const openDay = (date: string) => {
    setWeekStart(mondayStr(new Date(date + "T00:00:00")));
    setView("week");
  };

  const rows = useMemo(() => {
    if (!cal) return [];
    const out: number[] = [];
    for (let m = cal.openMinute; m + cal.slotMinutes <= cal.closeMinute; m += cal.slotMinutes) out.push(m);
    return out;
  }, [cal]);

  const now = Date.now();
  const openSlot = (dayIdx: number, rowIdx: number) => {
    if (!cal) return;
    const slot = cal.days[dayIdx].slots[rowIdx];
    if (slot.status !== "open" || new Date(slot.start).getTime() < now) return;
    navigate(`/facilities/${cal.facilityId}?start=${encodeURIComponent(slot.start)}&duration=${cal.minMinutes}`);
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-brand-600">{t("calendar.title")}</p>
          <select
            aria-label={t("calendar.facility")}
            value={facilityId}
            onChange={(e) => setParams({ facility: e.target.value })}
            className="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-base font-medium text-slate-800 outline-none focus:border-brand-500"
          >
            {facilities?.map((f) => <option key={f.id} value={f.id}>{f.name}</option>)}
          </select>
        </div>
        <div className="flex items-center gap-4">
          <Legend />
          <div role="group" aria-label={t("calendar.view")} className="flex overflow-hidden rounded-lg border border-slate-300 text-sm">
            {(["week", "month"] as const).map((v) => (
              <button
                key={v}
                type="button"
                onClick={() => setView(v)}
                aria-pressed={view === v}
                className={`px-3 py-1.5 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-500 ${view === v ? "bg-brand-500 text-white" : "bg-white text-slate-600 hover:bg-slate-50"}`}
              >
                {t(`calendar.${v}`)}
              </button>
            ))}
          </div>
        </div>
      </div>

      <Card className="p-4 sm:p-6">
        <div className="mb-4 flex items-center justify-between">
          <NavBtn dir="prev" onClick={goPrev} label={view === "week" ? t("calendar.prevWeek") : t("calendar.prevMonth")} />
          <h3 className="text-center text-sm font-semibold sm:text-base">
            {!cal ? "…" : view === "week"
              ? `${longDate(cal.days[0].date)} – ${longDate(cal.days[cal.days.length - 1].date)}`
              : monthLabel(weekStart)}
          </h3>
          <NavBtn dir="next" onClick={goNext} label={view === "week" ? t("calendar.nextWeek") : t("calendar.nextMonth")} />
        </div>

        {isLoading || !cal ? (
          <Spinner />
        ) : view === "month" ? (
          <MonthGrid cal={cal} month={weekStart.slice(0, 7)} onDay={openDay} />
        ) : (
          <div className="overflow-x-auto">
            <div
              className="grid min-w-[720px] gap-px rounded-lg bg-slate-200"
              style={{ gridTemplateColumns: `56px repeat(${cal.days.length}, minmax(0, 1fr))` }}
            >
              {/* header row */}
              <div className="bg-white" />
              {cal.days.map((d) => (
                <div key={d.date} className={`bg-white px-2 py-2 text-center text-sm font-medium ${d.isToday ? "text-red-600" : "text-slate-600"}`}>
                  {d.label}
                </div>
              ))}

              {/* slot rows */}
              {rows.map((minute, r) => (
                <RowGroup key={minute} minute={minute} dayCount={cal.days.length}>
                  {cal.days.map((d, di) => (
                    <SlotCell key={d.date} day={d} rowIdx={r} past={new Date(d.slots[r].start).getTime() < now} onOpen={() => openSlot(di, r)} openLabel={t("calendar.open")} />
                  ))}
                </RowGroup>
              ))}
            </div>
          </div>
        )}

        {cal && (
          <p className="mt-4 text-xs text-slate-500">
            {t("calendar.footer", {
              open: hourLabel(cal.openMinute),
              close: hourLabel(cal.closeMinute),
              buffer: cal.bufferMinutes,
              min: cal.minMinutes / 60,
            })}
          </p>
        )}
      </Card>
    </div>
  );
}

function RowGroup({ minute, children }: { minute: number; dayCount: number; children: React.ReactNode }) {
  return (
    <>
      <div className="flex items-center bg-white px-2 text-xs text-slate-500">{hourLabel(minute)}</div>
      {children}
    </>
  );
}

const statusClass: Record<SlotStatus, string> = {
  open: "bg-slate-100 hover:bg-brand-100",
  booked: "bg-red-500",
  blackout: "",
  closed: "bg-slate-50",
};

function SlotCell({ day, rowIdx, past, onOpen, openLabel }: { day: CalendarDay; rowIdx: number; past: boolean; onOpen: () => void; openLabel: string }) {
  const slot = day.slots[rowIdx];
  const clickable = slot.status === "open" && !past;
  const blackoutStyle = slot.status === "blackout"
    ? { backgroundImage: "var(--blackout-hatch)" }
    : undefined;

  if (clickable) {
    return (
      <button
        type="button"
        onClick={onOpen}
        aria-label={`${openLabel} · ${day.label} ${hourLabel(slotMinuteOfDay(slot.start))}`}
        className={`h-11 w-full ${statusClass.open} transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-500`}
      />
    );
  }
  return <div className={`h-11 w-full ${past && slot.status === "open" ? "bg-slate-100 opacity-50" : statusClass[slot.status]}`} style={blackoutStyle} aria-hidden="true" />;
}

function slotMinuteOfDay(iso: string): number {
  const d = new Date(iso);
  return d.getHours() * 60 + d.getMinutes();
}

function MonthGrid({ cal, month, onDay }: { cal: FacilityCalendar; month: string; onDay: (date: string) => void }) {
  const now = Date.now();
  const weekdays = cal.days.slice(0, 7).map((d) => d.label.split(" ")[0]);
  return (
    <div className="grid grid-cols-7 gap-px rounded-lg bg-slate-200">
      {weekdays.map((w) => (
        <div key={w} className="bg-white py-2 text-center text-xs font-medium text-slate-500">{w}</div>
      ))}
      {cal.days.map((d) => {
        const inMonth = d.date.slice(0, 7) === month;
        const open = d.slots.filter((s) => s.status === "open" && new Date(s.start).getTime() > now).length;
        const booked = d.slots.filter((s) => s.status === "booked").length;
        return (
          <button
            key={d.date}
            type="button"
            onClick={() => onDay(d.date)}
            className={`flex min-h-[68px] flex-col items-start gap-1 p-2 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-500 ${inMonth ? "bg-white hover:bg-brand-50" : "bg-slate-50"}`}
          >
            <span className={`text-sm font-medium ${d.isToday ? "text-red-600" : inMonth ? "text-slate-700" : "text-slate-400"}`}>
              {Number(d.date.slice(8))}
            </span>
            {inMonth && (
              <>
                <div className="flex h-1.5 w-full overflow-hidden rounded-full bg-slate-100">
                  <div className="bg-red-400" style={{ width: `${(booked / Math.max(1, booked + open)) * 100}%` }} />
                </div>
                <span className="text-[11px] text-slate-500">{open} open</span>
              </>
            )}
          </button>
        );
      })}
    </div>
  );
}

function Legend() {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-4 text-xs text-slate-600">
      <span className="flex items-center gap-1.5"><span className="h-3.5 w-3.5 rounded-sm border border-slate-300 bg-slate-100" />{t("calendar.open")}</span>
      <span className="flex items-center gap-1.5"><span className="h-3.5 w-3.5 rounded-sm bg-red-500" />{t("calendar.booked")}</span>
      <span className="flex items-center gap-1.5"><span className="h-3.5 w-3.5 rounded-sm" style={{ backgroundImage: "var(--blackout-hatch-sm)" }} />{t("calendar.blackout")}</span>
    </div>
  );
}

function NavBtn({ dir, onClick, label }: { dir: "prev" | "next"; onClick: () => void; label: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className="grid h-9 w-9 place-items-center rounded-lg border border-slate-300 text-slate-600 hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
    >
      {dir === "prev" ? "‹" : "›"}
    </button>
  );
}
