import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { api, type Booking } from "../lib/api";
import { todayISO } from "../lib/day";
import { Badge, Button, Card, Input, Spinner, StatusBadge, formatDateTime, formatFee, formatTime } from "../components/ui";

export function BookingDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const [rescheduling, setRescheduling] = useState(false);
  const { data: b, isLoading } = useQuery({ queryKey: ["booking", id], queryFn: () => api.myBookings().then((list) => list.find((x) => x.id === id)) });

  if (isLoading) return <Spinner />;
  if (!b) return <p className="text-red-600">{t("booking.notFound")}</p>;

  // Payment is offered once the booking is entitled to be paid for — never
  // while it is pending, because approval comes before money: charging a
  // resident before staff decide means a denial owes a refund this app cannot
  // issue (FAC-52).
  const needsPayment =
    b.feeCents > 0 &&
    b.payment?.status !== "paid" &&
    (b.status === "awaiting_payment" || b.status === "conditional");
  const upcoming = new Date(b.startsAt) > new Date();
  // A hold is the resident's to abandon: cancelling one takes no money and owes
  // no refund, since nothing has been paid.
  const canModify = upcoming && (b.status === "pending" || b.status === "awaiting_payment" || b.status === "confirmed");

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <Link to="/my-bookings" className="text-sm text-brand-600 hover:underline">{t("booking.back")}</Link>

      <Card className="space-y-4 p-6">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-2xl font-semibold">{b.facility?.name ?? "Facility"}</h2>
            <p className="text-slate-500">{b.facility?.location}</p>
          </div>
          <StatusBadge status={b.status} />
        </div>

        <dl className="grid gap-2 text-sm text-slate-600 sm:grid-cols-2">
          <Field label={t("booking.when")} value={`${formatDateTime(b.startsAt)} – ${formatDateTime(b.endsAt)}`} />
          <Field label={t("booking.purpose")} value={b.purpose || "—"} />
          <Field label={t("booking.attendance")} value={String(b.attendance)} />
          <Field label={t("booking.fee")} value={formatFee(b.feeCents)} />
        </dl>

        {b.status === "pending" && (
          <div className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
            {t("booking.pendingMsg")}
          </div>
        )}
        {b.status === "awaiting_payment" && (
          <div className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
            {t("booking.awaitingPaymentMsg")}
          </div>
        )}
        {b.status === "confirmed" && (
          <div className="flex items-center justify-between rounded-lg bg-green-50 p-3 text-sm text-green-800">
            <span>{t("booking.confirmedMsg")}</span>
            <a href={api.inviteUrl(b.id)} className="font-medium underline">{t("booking.downloadInvite")}</a>
          </div>
        )}

        {canModify && (
          <div className="border-t border-slate-100 pt-4">
            <Button variant="outline" onClick={() => setRescheduling((v) => !v)}>
              {rescheduling ? t("booking.cancelChange") : t("booking.changeTime")}
            </Button>
          </div>
        )}
      </Card>

      {b.status === "conditional" && <ConditionsCard booking={b} />}
      {rescheduling && <RescheduleCard booking={b} onDone={() => setRescheduling(false)} />}
      {b.facility?.requiresWaiver && <WaiverCard booking={b} />}
      {needsPayment && <PaymentCard booking={b} />}
      {b.payment?.status === "paid" && (
        <Card className="flex items-center justify-between p-6">
          <span className="text-sm text-slate-600">{t("booking.paymentReceived")}</span>
          <Badge tone="green">{t("booking.paid", { price: formatFee(b.payment.amountCents) })}</Badge>
        </Card>
      )}
      {b.payment?.status === "refunded" && (
        <Card className="flex items-center justify-between p-6">
          <span className="text-sm text-slate-600">{t("booking.refunded")}</span>
          <Badge tone="slate">{t("booking.refunded")}</Badge>
        </Card>
      )}
    </div>
  );
}

// ConditionsCard shows a conditionally-approved booking's terms and exactly what
// is still outstanding (§4.5).
//
// The list is the point. "Not confirmed yet" is not something a resident can
// act on; "accept the terms, pay $50, upload proof of insurance" is. The slot is
// held throughout, and saying so removes the reason to panic.
function ConditionsCard({ booking }: { booking: Booking }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: outstanding } = useQuery({
    queryKey: ["conditions", booking.id],
    queryFn: () => api.bookingConditions(booking.id),
  });

  const accept = useMutation({
    mutationFn: () => api.acceptConditions(booking.id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["conditions", booking.id] });
      void qc.invalidateQueries({ queryKey: ["booking", booking.id] });
      void qc.invalidateQueries({ queryKey: ["myBookings"] });
    },
  });

  const c = booking.condition;

  return (
    <Card className="space-y-4 p-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-semibold">{t("booking.conditionsTitle")}</h3>
        <Badge tone="amber">{t("booking.conditionsBadge")}</Badge>
      </div>

      <p className="text-sm text-slate-600">{t("booking.conditionsHeld")}</p>

      {c?.terms && (
        <div className="rounded-lg bg-slate-50 p-3">
          <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">{t("booking.conditionsTerms")}</p>
          <p className="mt-1 text-sm text-slate-700">{c.terms}</p>
        </div>
      )}

      <div>
        <p className="mb-2 text-sm font-medium">{t("booking.conditionsOutstanding")}</p>
        <ul className="space-y-1.5 text-sm">
          {outstanding?.acceptTerms && <li className="text-amber-800">• {t("booking.outstandingTerms")}</li>}
          {!!outstanding?.payCents && <li className="text-amber-800">• {t("booking.outstandingPay", { amount: formatFee(outstanding.payCents) })}</li>}
          {outstanding?.uploadLabel && <li className="text-amber-800">• {t("booking.outstandingUpload", { label: outstanding.uploadLabel })}</li>}
          {outstanding?.allSatisfied && <li className="text-green-700">• {t("booking.outstandingNone")}</li>}
        </ul>
      </div>

      {outstanding?.acceptTerms && (
        <Button disabled={accept.isPending} onClick={() => accept.mutate()}>
          {accept.isPending ? t("booking.accepting") : t("booking.acceptConditions")}
        </Button>
      )}
    </Card>
  );
}

// RescheduleCard lets the booker move a booking to another free slot, keeping
// the original duration (§4.9).
function RescheduleCard({ booking, onDone }: { booking: Booking; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const durationMs = new Date(booking.endsAt).getTime() - new Date(booking.startsAt).getTime();
  const [date, setDate] = useState(booking.startsAt.slice(0, 10));
  const [startISO, setStartISO] = useState("");
  const [error, setError] = useState("");

  const { data: slots, isLoading } = useQuery({
    queryKey: ["availability", booking.facilityId, date],
    queryFn: () => api.availability(booking.facilityId, date),
  });
  const free = useMemo(() => (slots ?? []).filter((s) => s.available), [slots]);

  const move = useMutation({
    mutationFn: () => {
      const start = new Date(startISO);
      return api.reschedule(booking.id, { start: start.toISOString(), end: new Date(start.getTime() + durationMs).toISOString() });
    },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["booking", booking.id] }); void qc.invalidateQueries({ queryKey: ["myBookings"] }); onDone(); },
    onError: (e: Error) => setError(e.message),
  });

  return (
    <Card className="space-y-4 p-6">
      <h3 className="font-semibold">{t("booking.changeTime")}</h3>
      <label className="block text-sm">
        <span className="mb-1 block text-slate-500">{t("facility.date")}</span>
        <Input type="date" min={todayISO()} value={date} onChange={(e) => { setDate(e.target.value); setStartISO(""); }} />
      </label>
      <div>
        <span className="mb-1 block text-sm text-slate-500">{t("booking.keepsDuration", { hours: Math.round(durationMs / 3.6e6 * 10) / 10 })}</span>
        {isLoading ? (
          <Spinner label={t("facility.checkingAvailability")} />
        ) : free.length ? (
          <div className="grid grid-cols-4 gap-2">
            {free.map((s) => (
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
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      <div className="flex gap-2">
        <Button disabled={!startISO || move.isPending} onClick={() => { setError(""); move.mutate(); }}>
          {move.isPending ? t("booking.moving") : t("booking.confirmNewTime")}
        </Button>
        <Button variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      </div>
    </Card>
  );
}

// WaiverCard handles the required waiver / proof-of-insurance upload (§4.11).
function WaiverCard({ booking }: { booking: Booking }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [error, setError] = useState("");
  const upload = useMutation({
    mutationFn: (file: File) => api.uploadWaiver(booking.id, file),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["booking", booking.id] }); void qc.invalidateQueries({ queryKey: ["myBookings"] }); },
    onError: (e: Error) => setError(e.message),
  });

  return (
    <Card className="space-y-3 p-6">
      <h3 className="font-semibold">{t("booking.waiverTitle")}</h3>
      {booking.waiver ? (
        <div className="flex items-center justify-between rounded-lg bg-green-50 p-3 text-sm text-green-800">
          <span>{t("booking.waiverReceived")}</span>
          <a href={api.waiverUrl(booking.id)} className="font-medium underline">{t("common.download")}</a>
        </div>
      ) : (
        <>
          <p className="text-sm text-slate-500">{t("booking.waiverHint")}</p>
          <a href={api.waiverTemplateUrl()} className="inline-block text-sm font-medium text-brand-600 hover:underline">
            {t("booking.exampleWaiver")} ↓
          </a>
          <input
            type="file"
            aria-label={t("a11y.uploadWaiver")}
            accept="application/pdf,image/png,image/jpeg,image/gif"
            disabled={upload.isPending}
            onChange={(e) => { const f = e.target.files?.[0]; if (f) { setError(""); upload.mutate(f); } }}
            className="block text-sm text-slate-600 file:mr-3 file:rounded-lg file:border-0 file:bg-brand-500 file:px-4 file:py-2 file:text-sm file:font-medium file:text-white hover:file:bg-brand-600"
          />
          {upload.isPending && <p role="status" className="text-sm text-slate-500">{t("booking.uploading")}</p>}
        </>
      )}
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
    </Card>
  );
}

// PaymentCard is a simulated Stripe checkout — the same test cards Stripe uses,
// but no keys and no real charge.
function PaymentCard({ booking }: { booking: Booking }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [card, setCard] = useState("4242 4242 4242 4242");
  const [error, setError] = useState("");
  const method = useQuery({ queryKey: ["paymentMethod"], queryFn: api.paymentMethod });

  const pay = useMutation({
    mutationFn: () => api.pay(booking.id, card),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["booking", booking.id] }); void qc.invalidateQueries({ queryKey: ["myBookings"] }); },
    onError: (e: Error) => setError(e.message),
  });

  // A hosted gateway (C2's payment broker) runs its own checkout, so there is no
  // card form to render here — the resident is sent away to pay and comes back.
  if (booking.payment?.payUrl) {
    return <HostedPaymentCard booking={booking} payUrl={booking.payment.payUrl} />;
  }

  // ...and before any bill exists there is no payUrl to read, so the gateway
  // has to be asked. Deciding from the payment alone meant the very first
  // payment attempt always rendered the simulated card form, whatever gateway
  // the municipality had configured: the resident typed a card number into a
  // form whose value the server then discarded.
  if (method.data?.hostedCheckout) {
    return <StartHostedPaymentCard booking={booking} gateway={method.data.name} />;
  }

  return (
    <Card className="space-y-4 p-6">
      <div className="flex items-center justify-between">
        <h3 className="font-semibold">{t("booking.pay", { price: formatFee(booking.feeCents) })}</h3>
        <span className="text-xs text-slate-500">{t("booking.secureDemo")}</span>
      </div>

      <label className="block text-sm">
        <span className="mb-1 block text-slate-500">{t("booking.cardNumber")}</span>
        <Input value={card} onChange={(e) => setCard(e.target.value)} inputMode="numeric" />
      </label>
      <div className="grid grid-cols-2 gap-3">
        <Input placeholder="MM / YY" defaultValue="12 / 28" />
        <Input placeholder="CVC" defaultValue="123" />
      </div>

      <p className="text-xs text-slate-500">{t("booking.testCards")}</p>
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}

      <Button className="w-full" disabled={pay.isPending} onClick={() => { setError(""); pay.mutate(); }}>
        {pay.isPending ? t("booking.processing") : t("booking.pay", { price: formatFee(booking.feeCents) })}
      </Button>
    </Card>
  );
}

// StartHostedPaymentCard raises the bill and sends the resident to the gateway.
//
// Raising it is what produces the payUrl, so the bill has to exist before the
// resident can be sent anywhere — that is the whole reason this is a button and
// not a link. The card argument is empty because a hosted gateway takes no card
// here; the server ignores it on this path.
function StartHostedPaymentCard({ booking, gateway }: { booking: Booking; gateway: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [error, setError] = useState("");

  const start = useMutation({
    mutationFn: () => api.pay(booking.id, ""),
    onSuccess: (p) => {
      void qc.invalidateQueries({ queryKey: ["booking", booking.id] });
      void qc.invalidateQueries({ queryKey: ["myBookings"] });
      // Same tab: the resident is mid-payment and comes back to the booking,
      // and a popup here is the thing a browser is most likely to block.
      if (p.payUrl) window.location.href = p.payUrl;
    },
    onError: (e: Error) => setError(e.message),
  });

  return (
    <Card className="space-y-4 p-6">
      <div className="flex items-center justify-between">
        <h3 className="font-semibold">{t("booking.pay", { price: formatFee(booking.feeCents) })}</h3>
        <span className="text-xs text-slate-500">{gateway}</span>
      </div>

      <p className="text-sm text-slate-600">{t("booking.hostedIntro")}</p>
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}

      <Button className="w-full" disabled={start.isPending} onClick={() => { setError(""); start.mutate(); }}>
        {start.isPending ? t("booking.processing") : t("booking.payAtPortal")}
      </Button>

      <p className="text-xs text-slate-500">{t("booking.hostedHold")}</p>
    </Card>
  );
}

// HostedPaymentCard sends the resident to the gateway's own checkout.
//
// It deliberately does not claim the booking is paid on return: settlement
// arrives on the server's callback, which may land after the resident is back.
// Saying "paid" here and being wrong is worse than saying "we are waiting".
function HostedPaymentCard({ booking, payUrl }: { booking: Booking; payUrl: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();

  return (
    <Card className="space-y-4 p-6">
      <div className="flex items-center justify-between">
        <h3 className="font-semibold">{t("booking.pay", { price: formatFee(booking.feeCents) })}</h3>
        <Badge tone="amber">{t("booking.awaitingPayment")}</Badge>
      </div>

      <p className="text-sm text-slate-600">{t("booking.hostedIntro")}</p>

      {/* rel=noopener because this leaves our origin for the payment portal. */}
      <a
        href={payUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex w-full items-center justify-center rounded-lg bg-brand-600 px-4 py-2 font-medium text-white hover:bg-brand-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
      >
        {t("booking.payAtPortal")}
      </a>

      <p className="text-xs text-slate-500">{t("booking.hostedHold")}</p>

      <Button
        variant="outline"
        className="w-full"
        onClick={() => {
          void qc.invalidateQueries({ queryKey: ["booking", booking.id] });
          void qc.invalidateQueries({ queryKey: ["myBookings"] });
        }}
      >
        {t("booking.checkPayment")}
      </Button>
    </Card>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-slate-500">{label}</dt>
      <dd className="font-medium text-slate-700">{value}</dd>
    </div>
  );
}
