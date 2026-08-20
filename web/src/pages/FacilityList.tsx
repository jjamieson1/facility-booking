import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { api, type FacilityFilter } from "../lib/api";
import { Badge, Button, Card, Input, Spinner, formatFee } from "../components/ui";

export function FacilityList() {
  const { t } = useTranslation();
  const [minCapacity, setMinCapacity] = useState("");
  const [free, setFree] = useState(false);
  const [date, setDate] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");

  const windowSet = Boolean(date && start && end);
  const filter: FacilityFilter = {
    minCapacity: minCapacity ? Number(minCapacity) : undefined,
    free: free || undefined,
    ...(windowSet ? { date, start, end } : {}),
  };
  const { data, isLoading } = useQuery({
    queryKey: ["facilities", filter],
    queryFn: () => api.listFacilities(filter),
  });

  const clearTime = () => {
    setDate("");
    setStart("");
    setEnd("");
  };

  return (
    <div>
      <h2 className="mb-1 text-2xl font-semibold">{t("list.title")}</h2>
      <p className="mb-6 text-slate-500">{t("list.subtitle")}</p>

      <Card className="mb-6 p-4">
        <div className="flex flex-wrap items-end gap-x-4 gap-y-3">
          <label className="text-sm">
            <span className="mb-1 block text-slate-500">{t("list.minCapacity")}</span>
            <Input type="number" min={0} value={minCapacity} onChange={(e) => setMinCapacity(e.target.value)} placeholder={t("list.any")} className="w-28" />
          </label>
          <label className="flex items-center gap-2 pb-2 text-sm text-slate-600">
            <input type="checkbox" checked={free} onChange={(e) => setFree(e.target.checked)} className="h-4 w-4" />
            {t("list.freeOnly")}
          </label>

          {/* Divider between parameter and date/time filters; collapses when the row wraps. */}
          <div className="mb-1 hidden h-9 w-px self-end bg-slate-200 lg:block" aria-hidden="true" />

          <label className="text-sm">
            <span className="mb-1 block text-slate-500">{t("list.date")}</span>
            <Input type="date" min={new Date().toISOString().slice(0, 10)} value={date} onChange={(e) => setDate(e.target.value)} className="w-40" />
          </label>
          <label className="text-sm">
            <span className="mb-1 block text-slate-500">{t("list.from")}</span>
            <Input type="time" step={1800} value={start} onChange={(e) => setStart(e.target.value)} className="w-28" />
          </label>
          <label className="text-sm">
            <span className="mb-1 block text-slate-500">{t("list.to")}</span>
            <Input type="time" step={1800} value={end} onChange={(e) => setEnd(e.target.value)} className="w-28" />
          </label>
          {(date || start || end) && (
            <Button variant="ghost" className="mb-1" onClick={clearTime}>{t("list.clearTime")}</Button>
          )}
        </div>
      </Card>

      {windowSet && (
        <p className="mb-4 text-sm text-slate-500">{t("list.showing", { date, start, end })}</p>
      )}

      {isLoading ? (
        <Spinner label={t("list.loading")} />
      ) : !data?.length ? (
        <Card className="p-8 text-center text-slate-500">
          {windowSet
            ? t("list.noneWindow")
            : t("list.none")}
        </Card>
      ) : (
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {data.map((f) => (
            <Link key={f.id} to={`/facilities/${f.id}`}>
              <Card className="h-full overflow-hidden transition hover:shadow-md">
                <img src={f.imageUrl} alt={f.name} className="h-40 w-full object-cover" />
                <div className="space-y-2 p-4">
                  <div className="flex items-start justify-between gap-2">
                    <h3 className="font-semibold">{f.name}</h3>
                    <Badge>{formatFee(f.feeCents)}</Badge>
                  </div>
                  <p className="line-clamp-2 text-sm text-slate-500">{f.description}</p>
                  <p className="text-xs text-slate-500">{t("list.upTo", { count: f.capacity })} · {f.location}</p>
                  {windowSet && <Badge tone="green">{t("list.freeAtThisTime")}</Badge>}
                </div>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
