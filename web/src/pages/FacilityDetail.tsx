import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { api, type Booking, type RecurringResult } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, FacilityImage, Input, Spinner, formatFee, formatTime } from "../components/ui";

function today() {
  return new Date().toISOString().slice(0, 10);
}

export function FacilityDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { user } = useAuth();
  const { data: f, isLoading } = useQuery({ queryKey: ["facility", id], queryFn: () => api.getFacility(id) });

  if (isLoading) return <Spinner />;
  if (!f) return <p className="text-red-600">{t("booking.notFound")}</p>;

  const isResident = !!user?.isResident;
  const hasResidentRate = f.nonResidentFeeCents > 0 && f.nonResidentFeeCents !== f.feeCents;
  const yourFee = isResident || !hasResidentRate ? f.feeCents : f.nonResidentFeeCents;

  return (
    <div className="space-y-6">
      <Link to="/" className="text-sm text-brand-600 hover:underline">{t("facility.all")}</Link>
      <FacilityImage src={f.imageUrl} alt={f.name} className="h-64 w-full rounded-xl object-cover" />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-3xl font-semibold">{f.name}</h2>
        <div className="flex items-center gap-2">
          <Badge>{formatFee(yourFee)}</Badge>
          {f.requiresApproval && <Badge tone="amber">{t("facility.staffApproval")}</Badge>}
        </div>
      </div>
      {hasResidentRate && (
        <p className="text-sm text-slate-500">
          {t("facility.residentRate", { price: formatFee(f.feeCents), price2: formatFee(f.nonResidentFeeCents) })}
          {!isResident && (user
            ? <> <Link to="/my-bookings" className="text-brand-600 underline">{t("facility.verifyResidency")}</Link>{t("facility.verifyResidencySuffix")}</>
            : t("facility.signInResidency"))}
        </p>
      )}
      <p className="text-slate-600">{f.description}</p>
      <Link to={`/availability?facility=${f.id}`} className="inline-flex items-center gap-1 text-sm font-medium text-brand-600 hover:underline">
        {t("facility.viewCalendar")} →
      </Link>

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="space-y-6 lg:col-span-2">
          <div className="grid gap-6 sm:grid-cols-2">
            <Card className="p-5">
              <h3 className="mb-3 font-semibold">{t("facility.details")}</h3>
              <dl className="space-y-1 text-sm text-slate-600">
                <Row label={t("facility.capacity")} value={t("list.upTo", { count: f.capacity })} />
                <Row label={t("facility.location")} value={f.location} />
                <Row label={t("facility.deposit")} value={f.depositCents ? formatFee(f.depositCents) : t("common.none")} />
                <Row label={t("facility.stepFree")} value={f.stepFreeAccess ? t("common.yes") : t("common.no")} />
                <Row label={t("facility.accessibleWashroom")} value={f.accessibleWashroom ? t("common.yes") : t("common.no")} />
              </dl>
            </Card>
            <Card className="p-5">
              <h3 className="mb-3 font-semibold">{t("facility.accessories")}</h3>
              {f.accessories?.length ? (
                <ul className="flex flex-wrap gap-2">
                  {f.accessories.map((a) => (
                    <li key={a.id}><Badge tone="slate">{a.accessory.name}{a.quantity > 1 ? ` ×${a.quantity}` : ""}</Badge></li>
                  ))}
                </ul>
              ) : (
                <p className="text-sm text-slate-500">{t("facility.noneListed")}</p>
              )}
            </Card>
          </div>
          <div className="grid gap-6 sm:grid-cols-2">
            <Card className="p-5">
              <h3 className="mb-2 font-semibold">{t("facility.beforeBooking")}</h3>
              <p className="text-sm text-slate-600">{f.beforeInstructions}</p>
            </Card>
            <Card className="p-5">
              <h3 className="mb-2 font-semibold">{t("facility.afterBooking")}</h3>
              <p className="text-sm text-slate-600">{f.afterInstructions}</p>
            </Card>
          </div>
          <UntranslatedNotice detail={f} />
          <CancellationTerms facilityId={f.id} />
        </div>

        <BookingWidget facilityId={f.id} minMinutes={60} requiresWaiver={f.requiresWaiver} />
      </div>
    </div>
  );
}

function BookingWidget({ facilityId, requiresWaiver }: { facilityId: string; minMinutes: number; requiresWaiver: boolean }) {
  const { t } = useTranslation();
  const { user, login } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  // A ?start=<ISO>&duration=<min> deep-link (e.g. from the availability calendar)
  // preselects that slot.
  const presetStart = searchParams.get("start");
  const presetDuration = Number(searchParams.get("duration"));
  const [date, setDate] = useState(presetStart ? presetStart.slice(0, 10) : today());
  const [startISO, setStartISO] = useState(presetStart ?? "");
  const [durationMin, setDurationMin] = useState([60, 120, 180, 240].includes(presetDuration) ? presetDuration : 60);
  const [purpose, setPurpose] = useState("");
  const [attendance, setAttendance] = useState(1);
  const [repeat, setRepeat] = useState(false);
  const [weeks, setWeeks] = useState(4);
  const [waiverFile, setWaiverFile] = useState<File | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const { data: slots, isLoading } = useQuery({
    queryKey: ["availability", facilityId, date],
    queryFn: () => api.availability(facilityId, date),
  });
  const available = useMemo(() => (slots ?? []).filter((s) => s.available), [slots]);
  const taken = useMemo(() => (slots ?? []).filter((s) => !s.available), [slots]);

  const waitlist = useMutation({
    mutationFn: (slot: { start: string; end: string }) => api.joinWaitlist(facilityId, slot),
    onSuccess: () => setNotice(t("facility.waitlistAdded")),
    onError: (e: Error) => setError(e.message),
  });

  const create = useMutation({
    mutationFn: async (): Promise<{ recurring: true; r: RecurringResult } | { recurring: false; b: Booking }> => {
      const start = new Date(startISO);
      const end = new Date(start.getTime() + durationMin * 60_000);
      const base = { facilityId, start: start.toISOString(), end: end.toISOString(), purpose, attendance };
      if (repeat && weeks > 1) {
        const r = await api.createRecurring({ ...base, repeatWeeks: weeks });
        // Best-effort: attach the same waiver to every session created. Any that
        // fail can still be uploaded later from the booking page.
        if (requiresWaiver && waiverFile) {
          await Promise.all((r.created ?? []).map((bk) => api.uploadWaiver(bk.id, waiverFile).catch(() => undefined)));
        }
        return { recurring: true, r };
      }
      const b = await api.createBooking(base);
      // Upload the waiver as part of booking so a no-approval facility confirms
      // immediately. On failure we still land on the booking page to retry.
      if (requiresWaiver && waiverFile) {
        await api.uploadWaiver(b.id, waiverFile).catch(() => undefined);
      }
      return { recurring: false, b };
    },
    onSuccess: (out) => {
      if (out.recurring) {
        const created = out.r.created ?? [];
        const skipped = out.r.skipped ?? [];
        if (created.length === 0) {
          setError(t("facility.noWeeks"));
          return;
        }
        const skippedText = skipped.length ? t("facility.skippedSuffix", { count: skipped.length }) : "";
        setNotice(t("facility.bookedSessions", { count: created.length, plural: created.length > 1 ? "s" : "", skipped: skippedText }));
        setTimeout(() => navigate("/my-bookings"), 1200);
      } else {
        navigate(`/bookings/${out.b.id}`);
      }
    },
    onError: (e: Error) => setError(e.message),
  });

  if (!user) {
    return (
      <Card className="h-fit p-5">
        <h3 className="mb-2 font-semibold">{t("facility.book")}</h3>
        <p className="mb-4 text-sm text-slate-500">{t("facility.signInHint")}</p>
        <Button onClick={login} className="w-full">{t("auth.signInToBook")}</Button>
      </Card>
    );
  }

  return (
    <Card className="h-fit space-y-4 p-5">
      <h3 className="font-semibold">{t("facility.book")}</h3>

      <label className="block text-sm">
        <span className="mb-1 block text-slate-500">{t("facility.date")}</span>
        <Input type="date" min={today()} value={date} onChange={(e) => { setDate(e.target.value); setStartISO(""); }} />
      </label>

      <div>
        <span className="mb-1 block text-sm text-slate-500">{t("facility.startTime")}</span>
        {isLoading ? (
          <Spinner label={t("facility.checkingAvailability")} />
        ) : available.length ? (
          <div className="grid grid-cols-3 gap-2">
            {available.map((s) => (
              <button
                key={s.start}
                type="button"
                aria-pressed={startISO === s.start}
                onClick={() => setStartISO(s.start)}
                className={`rounded-lg border px-2 py-1.5 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 ${startISO === s.start ? "border-brand-500 bg-brand-50 text-brand-700" : "border-slate-300 hover:bg-slate-50"}`}
              >
                {formatTime(s.start)}
              </button>
            ))}
          </div>
        ) : (
          <p className="text-sm text-slate-500">{t("facility.noFreeTimes")}</p>
        )}
      </div>

      {taken.length > 0 && (
        <div>
          <span className="mb-1 block text-sm text-slate-500">{t("facility.takenWaitlist")}</span>
          <div className="flex flex-wrap gap-2">
            {taken.map((s) => (
              <button
                key={s.start}
                type="button"
                disabled={waitlist.isPending}
                onClick={() => { setError(""); setNotice(""); waitlist.mutate({ start: s.start, end: s.end }); }}
                className="rounded-lg border border-dashed border-slate-300 px-2 py-1.5 text-sm text-slate-500 hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
                aria-label={t("a11y.joinWaitlist", { time: formatTime(s.start) })}
              >
                {formatTime(s.start)} <span aria-hidden="true">⏳</span>
              </button>
            ))}
          </div>
        </div>
      )}

      <label className="block text-sm">
        <span className="mb-1 block text-slate-500">{t("facility.duration")}</span>
        <select
          value={durationMin}
          onChange={(e) => setDurationMin(Number(e.target.value))}
          className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
        >
          {[60, 120, 180, 240].map((m) => (
            <option key={m} value={m}>{t("facility.hours", { count: m / 60 })}</option>
          ))}
        </select>
      </label>

      <label className="block text-sm">
        <span className="mb-1 block text-slate-500">{t("facility.purpose")}</span>
        <Input value={purpose} onChange={(e) => setPurpose(e.target.value)} placeholder={t("facility.purposePlaceholder")} />
      </label>

      <label className="block text-sm">
        <span className="mb-1 block text-slate-500">{t("facility.attendance")}</span>
        <Input type="number" min={1} value={attendance} onChange={(e) => setAttendance(Number(e.target.value))} />
      </label>

      {requiresWaiver && (
        <div className="space-y-2 rounded-lg border border-amber-200 bg-amber-50 p-3">
          <p className="text-sm font-medium text-amber-900">{t("facility.waiverRequired")}</p>
          <p className="text-xs text-amber-800">{t("facility.waiverUploadHint")}</p>
          <a href={api.waiverTemplateUrl()} className="inline-block text-sm font-medium text-brand-600 hover:underline">
            {t("booking.exampleWaiver")} ↓
          </a>
          <input
            type="file"
            aria-label={t("a11y.uploadWaiver")}
            accept="application/pdf,image/png,image/jpeg,image/gif"
            onChange={(e) => setWaiverFile(e.target.files?.[0] ?? null)}
            className="block w-full text-sm text-slate-600 file:mr-3 file:rounded-lg file:border-0 file:bg-brand-500 file:px-4 file:py-2 file:text-sm file:font-medium file:text-white hover:file:bg-brand-600"
          />
          {waiverFile
            ? <p className="text-xs text-green-700">{t("facility.waiverAttached", { name: waiverFile.name })}</p>
            : <p className="text-xs text-amber-800">{t("facility.waiverLater")}</p>}
        </div>
      )}

      <div className="rounded-lg border border-slate-200 p-3">
        <label className="flex items-center gap-2 text-sm text-slate-700">
          <input type="checkbox" className="h-4 w-4" checked={repeat} onChange={(e) => setRepeat(e.target.checked)} />
          {t("facility.repeatWeekly")}
        </label>
        {repeat && (
          <label className="mt-2 block text-sm">
            <span className="mb-1 block text-slate-500">{t("facility.numberOfWeeks")}</span>
            <Input type="number" min={2} max={52} value={weeks} onChange={(e) => setWeeks(Number(e.target.value))} className="w-28" />
          </label>
        )}
      </div>

      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      {notice && <p role="status" className="text-sm text-green-700">{notice}</p>}

      <Button
        className="w-full"
        disabled={!startISO || create.isPending}
        onClick={() => { setError(""); setNotice(""); create.mutate(); }}
      >
        {create.isPending ? t("facility.requesting") : repeat ? t("facility.bookWeekly", { count: weeks }) : t("facility.requestBooking")}
      </Button>
    </Card>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="text-slate-500">{label}</dt>
      <dd className="text-right">{value}</dd>
    </div>
  );
}

// CancellationTerms shows what happens if the resident cancels — on the page
// where they decide to book, not after. §4.7 requires the fee and its conditions
// to be visible before committing, and "you can get half back up to 48 hours
// out" is part of the price.
function CancellationTerms({ facilityId }: { facilityId: string }) {
  const { t } = useTranslation();
  const { data: policy } = useQuery({
    queryKey: ["cancellationPolicy", facilityId],
    queryFn: () => api.cancellationPolicy(facilityId),
  });
  if (!policy) return null;

  // Most generous first, which is the order someone reads them in.
  const tiers = [...(policy.tiers ?? [])].sort((a, b) => b.hoursBefore - a.hoursBefore);

  return (
    <Card className="p-5">
      <h3 className="mb-2 font-semibold">{t("facility.cancellationTerms")}</h3>
      {tiers.length === 0 ? (
        <p className="text-sm text-slate-600">{t("facility.noRefundTiers")}</p>
      ) : (
        <ul className="list-disc space-y-1 pl-5 text-sm text-slate-600">
          {tiers.map((tier) => (
            <li key={tier.hoursBefore}>
              {t("facility.refundTier", { hours: describeHours(tier.hoursBefore), percent: tier.refundPercent })}
            </li>
          ))}
        </ul>
      )}
      {policy.nonRefundableCents > 0 && (
        <p className="mt-2 text-sm text-slate-500">
          {t("facility.nonRefundable", { amount: formatFee(policy.nonRefundableCents) })}
        </p>
      )}
      {policy.modificationCutoffHours > 0 && (
        <p className="mt-2 text-sm text-slate-500">
          {t("facility.changeCutoff", { hours: policy.modificationCutoffHours })}
        </p>
      )}
    </Card>
  );
}

// describeHours renders an hour count the way the terms would be spoken.
function describeHours(hours: number): string {
  if (hours % 24 === 0 && hours >= 24) {
    const days = hours / 24;
    return days === 1 ? "1 day" : `${days} days`;
  }
  return hours === 1 ? "1 hour" : `${hours} hours`;
}

// UntranslatedNotice tells a reader which text they are seeing in the other
// language. §4.11's acceptance criterion is explicit: fall back to English, but
// say so — silently serving English under a French heading is what makes a
// bilingual claim untrue.
function UntranslatedNotice({ detail }: { detail: { language?: string; untranslated?: string[] } }) {
  const { t } = useTranslation();
  const missing = detail.untranslated ?? [];
  if (detail.language !== "fr" || missing.length === 0) return null;
  return (
    <Card className="p-4">
      <p role="status" className="text-sm text-slate-600">
        {t("facility.untranslated", { count: missing.length })}
      </p>
    </Card>
  );
}
