import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { api, type Facility, type FacilityFilter } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, FacilityImage, Input, Spinner, formatFee } from "../components/ui";

export function FacilityList() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const isResident = !!user?.isResident;
  const [minCapacity, setMinCapacity] = useState("");
  const [free, setFree] = useState(false);
  const [date, setDate] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  // §4.3's remaining parameters, behind a disclosure so the common case (a
  // capacity and a time) stays a single uncluttered row.
  const [showMore, setShowMore] = useState(false);
  const [area, setArea] = useState("");
  const [maxCost, setMaxCost] = useState("");
  const [stepFree, setStepFree] = useState(false);
  const [accessibleWashroom, setAccessibleWashroom] = useState(false);
  const [accessories, setAccessories] = useState<string[]>([]);

  // Options come from the whole directory, not the current results: choices that
  // disappeared as you filtered would leave you unable to undo them.
  const { data: options } = useQuery({ queryKey: ["facilityFilterOptions"], queryFn: api.facilityFilterOptions });

  const windowSet = Boolean(date && start && end);
  const filter: FacilityFilter = {
    minCapacity: minCapacity ? Number(minCapacity) : undefined,
    free: free || undefined,
    area: area || undefined,
    stepFree: stepFree || undefined,
    accessibleWashroom: accessibleWashroom || undefined,
    maxFeeCents: maxCost ? Math.round(Number(maxCost) * 100) : undefined,
    accessories: accessories.length ? accessories : undefined,
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

  const toggleAccessory = (name: string) =>
    setAccessories((prev) => (prev.includes(name) ? prev.filter((a) => a !== name) : [...prev, name]));

  const extraCount =
    (area ? 1 : 0) + (maxCost ? 1 : 0) + (stepFree ? 1 : 0) + (accessibleWashroom ? 1 : 0) + accessories.length;

  const clearExtras = () => {
    setArea("");
    setMaxCost("");
    setStepFree(false);
    setAccessibleWashroom(false);
    setAccessories([]);
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

          <Button
            variant="outline"
            className="mb-1 ml-auto"
            aria-expanded={showMore}
            aria-controls="more-filters"
            onClick={() => setShowMore((v) => !v)}
          >
            {showMore ? t("list.fewerFilters") : t("list.moreFilters")}
            {/* The bare number reads as "(3)" to a screen reader, which says
                nothing; the count gets spelled out for that case. */}
            {extraCount > 0 && !showMore && (
              <>
                <span aria-hidden="true" className="ml-1">({extraCount})</span>
                <span className="sr-only">
                  {" — "}
                  {t(extraCount === 1 ? "list.filterCount" : "list.filterCountPlural", { count: extraCount })}
                </span>
              </>
            )}
          </Button>
        </div>

        {showMore && (
          <div id="more-filters" className="mt-4 space-y-4 border-t border-slate-200 pt-4">
            <div className="flex flex-wrap items-end gap-x-4 gap-y-3">
              <label className="text-sm">
                <span className="mb-1 block text-slate-500">{t("list.area")}</span>
                <select
                  value={area}
                  onChange={(e) => setArea(e.target.value)}
                  className="h-10 w-48 rounded-lg border border-slate-300 bg-white px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
                >
                  <option value="">{t("list.anyArea")}</option>
                  {(options?.areas ?? []).map((a) => <option key={a} value={a}>{a}</option>)}
                </select>
              </label>
              <label className="text-sm">
                <span className="mb-1 block text-slate-500">{t("list.maxCost")}</span>
                <Input
                  type="number"
                  min={0}
                  step={5}
                  value={maxCost}
                  onChange={(e) => setMaxCost(e.target.value)}
                  placeholder={t("list.maxCostPlaceholder")}
                  className="w-32"
                />
              </label>
              <fieldset className="text-sm">
                <legend className="mb-1 block text-slate-500">{t("list.accessibility")}</legend>
                <div className="flex flex-wrap gap-4 pb-2 text-slate-600">
                  <label className="flex items-center gap-2">
                    <input type="checkbox" checked={stepFree} onChange={(e) => setStepFree(e.target.checked)} className="h-4 w-4" />
                    {t("list.stepFree")}
                  </label>
                  <label className="flex items-center gap-2">
                    <input type="checkbox" checked={accessibleWashroom} onChange={(e) => setAccessibleWashroom(e.target.checked)} className="h-4 w-4" />
                    {t("list.accessibleWashroom")}
                  </label>
                </div>
              </fieldset>
            </div>

            {!!options?.accessories.length && (
              <fieldset className="text-sm">
                <legend className="block text-slate-500">{t("list.accessoriesLabel")}</legend>
                <p className="mb-2 text-xs text-slate-500">{t("list.accessoriesHint")}</p>
                <div className="flex flex-wrap gap-x-4 gap-y-2 text-slate-600">
                  {options.accessories.map((a) => (
                    <label key={a} className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={accessories.includes(a)}
                        onChange={() => toggleAccessory(a)}
                        className="h-4 w-4"
                      />
                      {a}
                    </label>
                  ))}
                </div>
              </fieldset>
            )}

            {extraCount > 0 && (
              <Button variant="ghost" onClick={clearExtras}>{t("list.clearFilters")}</Button>
            )}
          </div>
        )}
      </Card>

      {/* Which price the cards show, said out loud — the cost filter compares
          against this same number, so a non-resident filtering at $100 needs to
          know the figures they are looking at are theirs. */}
      {maxCost && (
        <p className="mb-4 text-sm text-slate-500">
          {isResident ? t("list.yourRate") : t("list.yourRateNonResident")}
        </p>
      )}

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
                <FacilityImage src={f.imageUrl} alt={f.name} className="h-40 w-full object-cover" />
                <div className="space-y-2 p-4">
                  <div className="flex items-start justify-between gap-2">
                    <h3 className="font-semibold">{f.name}</h3>
                    <Badge>{formatFee(viewerFee(f, isResident))}</Badge>
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

// viewerFee is the price this viewer would actually be charged, mirroring
// domain.Facility.FeeFor and the SQL the cost filter uses. The card used to
// show the resident rate to everyone, which was already wrong on the detail
// page's terms — and once a cost filter exists, a card priced differently from
// the filter that selected it reads as a bug.
function viewerFee(f: Facility, isResident: boolean): number {
  const hasResidentRate = f.nonResidentFeeCents > 0 && f.nonResidentFeeCents !== f.feeCents;
  return isResident || !hasResidentRate ? f.feeCents : f.nonResidentFeeCents;
}
