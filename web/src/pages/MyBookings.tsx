import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, Input, StatusBadge, Spinner, formatDateTime, formatFee } from "../components/ui";

export function MyBookings() {
  const { t } = useTranslation();
  const { user, login } = useAuth();
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({ queryKey: ["myBookings"], queryFn: api.myBookings, enabled: !!user });

  const { data: waitlist } = useQuery({ queryKey: ["myWaitlist"], queryFn: api.myWaitlist, enabled: !!user });
  const leave = useMutation({
    mutationFn: (id: string) => api.leaveWaitlist(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["myWaitlist"] }),
  });

  if (!user) {
    return (
      <Card className="p-8 text-center">
        <p className="mb-4 text-slate-500">{t("mybookings.signInPrompt")}</p>
        <Button onClick={login}>{t("auth.signIn")}</Button>
      </Card>
    );
  }
  if (isLoading) return <Spinner />;

  return (
    <div>
      <h2 className="mb-6 text-2xl font-semibold">{t("mybookings.title")}</h2>

      <ResidencyCard />

      {!data?.length ? (
        <Card className="p-8 text-center text-slate-500">
          {t("mybookings.empty")} <Link to="/" className="text-brand-600 underline">{t("mybookings.browse")}</Link>{t("mybookings.emptySuffix")}
        </Card>
      ) : (
        <div className="space-y-3">
          {data.map((b) => {
            const upcoming = new Date(b.startsAt) > new Date();
            const cancellable = upcoming && (b.status === "pending" || b.status === "confirmed");
            return (
              <Card key={b.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
                <div>
                  <Link to={`/bookings/${b.id}`} className="font-semibold hover:underline">{b.facility?.name ?? "Facility"}</Link>
                  <p className="text-sm text-slate-500">{formatDateTime(b.startsAt)} · {formatFee(b.feeCents)}</p>
                </div>
                <div className="flex items-center gap-3">
                  {b.payment?.status === "paid" && <span className="text-xs text-green-700">{t("mybookings.paid")}</span>}
                  <StatusBadge status={b.status} />
                  <Link to={`/bookings/${b.id}`}><Button variant="outline">{t("common.view")}</Button></Link>
                  {cancellable && <CancelButton bookingId={b.id} />}
                </div>
              </Card>
            );
          })}
        </div>
      )}

      {!!waitlist?.length && (
        <div className="mt-10" data-marker="waitlist">
          <h3 className="mb-3 text-lg font-semibold">{t("mybookings.waitlist")}</h3>
          <p className="mb-3 text-sm text-slate-500">{t("mybookings.waitlistHint")}</p>
          <div className="space-y-2">
            {waitlist.map((e) => (
              <Card key={e.id} className="flex items-center justify-between p-4">
                <p className="text-sm">
                  <span className="font-medium">{e.facility?.name ?? "Facility"}</span> · {formatDateTime(e.startsAt)}
                </p>
                <Button variant="ghost" disabled={leave.isPending} onClick={() => leave.mutate(e.id)}>{t("common.leave")}</Button>
              </Card>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ResidencyCard submits evidence for the residency entitlement and reports what
// the provider decided. The form renders from the provider's published
// descriptor rather than from fields hardcoded here, so a municipality that asks
// for something other than an address needs no SPA change.
//
// Note what this component no longer does: it does not tell the server the user
// is a resident. It submits inputs; the provider decides.
// CancelButton asks first, and states the consequence in money before it does
// anything. §4.7 requires the policy to be applied on cancellation; showing the
// figure beforehand is what stops a resident discovering it on a statement.
function CancelButton({ bookingId }: { bookingId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [outcome, setOutcome] = useState<string>("");

  // Only quoted once the resident reaches for cancel — no point asking the
  // server what every listed booking would refund.
  const { data: quote, isLoading } = useQuery({
    queryKey: ["refundQuote", bookingId],
    queryFn: () => api.refundQuote(bookingId),
    enabled: confirming,
  });

  const cancel = useMutation({
    mutationFn: () => api.cancelBooking(bookingId),
    onSuccess: (res) => {
      setConfirming(false);
      setOutcome(
        res.refund && res.refund.refundCents > 0
          ? t("mybookings.refundIssued", { amount: formatFee(res.refund.refundCents) })
          : t("mybookings.noRefund"),
      );
      void qc.invalidateQueries({ queryKey: ["myBookings"] });
    },
  });

  if (outcome) return <span role="status" className="text-sm text-slate-600">{outcome}</span>;

  if (!confirming) {
    return (
      <Button variant="ghost" onClick={() => setConfirming(true)}>{t("mybookings.cancel")}</Button>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <span role="status" className="text-sm text-slate-600">
        {isLoading ? t("common.loading") : quote?.explanation}
        {quote && quote.refundCents > 0 && (
          <strong className="ml-1">{t("mybookings.willRefund", { amount: formatFee(quote.refundCents) })}</strong>
        )}
      </span>
      <Button variant="danger" disabled={cancel.isPending || isLoading} onClick={() => cancel.mutate()}>
        {cancel.isPending ? t("mybookings.cancelling") : t("mybookings.confirmCancel")}
      </Button>
      <Button variant="outline" onClick={() => setConfirming(false)}>{t("mybookings.keepBooking")}</Button>
    </div>
  );
}

function ResidencyCard() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const qc = useQueryClient();
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [denied, setDenied] = useState(false);

  const { data: ent } = useQuery({ queryKey: ["entitlements"], queryFn: api.entitlements, enabled: !!user });
  const { data: descriptor } = useQuery({
    queryKey: ["entitlementDescriptor", "residency"],
    queryFn: () => api.entitlementDescriptor("residency"),
    enabled: !!user,
  });

  const prove = useMutation({
    mutationFn: () => api.proveEntitlement("residency", inputs),
    onSuccess: (res) => {
      setDenied(res.outcome === "denied");
      void qc.invalidateQueries({ queryKey: ["entitlements"] });
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });

  if (!user) return null;

  const residency = ent?.live.find((d) => d.type === "residency");
  if (residency) {
    return (
      <Card className="mb-6 flex flex-wrap items-center justify-between gap-2 p-4">
        <span className="text-sm text-slate-600">{t("mybookings.residency")}</span>
        <span className="flex items-center gap-2">
          <Badge tone="green">{t("mybookings.verifiedResident")}</Badge>
          {/* A determination served from cache during a provider outage still
              applies; saying so beats implying it was freshly confirmed. */}
          {residency.stale && <Badge tone="amber">{t("mybookings.residencyStale")}</Badge>}
        </span>
      </Card>
    );
  }

  const unavailable = ent?.notices.some((n) => n.type === "residency" && n.reason === "unavailable");
  const required = descriptor?.fields.filter((f) => f.required) ?? [];
  const incomplete = required.some((f) => !(inputs[f.key] ?? "").trim());

  return (
    <Card className="mb-6 space-y-3 p-5">
      <div>
        <h3 className="font-semibold">{t("mybookings.verifyTitle")}</h3>
        <p className="text-sm text-slate-500">{descriptor?.statement ?? t("mybookings.verifyHint")}</p>
      </div>

      {denied && <p role="alert" className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">{t("mybookings.residencyDenied")}</p>}
      {unavailable && <p role="status" className="rounded-lg bg-slate-100 p-3 text-sm text-slate-700">{t("mybookings.residencyUnavailable")}</p>}

      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => { e.preventDefault(); if (!incomplete) prove.mutate(); }}
      >
        {(descriptor?.fields ?? []).map((f) => (
          <label key={f.key} className="text-sm">
            <span className="mb-1 block text-slate-500">{f.label}</span>
            <Input
              value={inputs[f.key] ?? ""}
              placeholder={f.placeholder}
              required={f.required}
              className="w-80"
              onChange={(e) => setInputs((c) => ({ ...c, [f.key]: e.target.value }))}
            />
          </label>
        ))}
        <Button type="submit" disabled={incomplete || prove.isPending}>
          {prove.isPending ? t("mybookings.verifying") : t("mybookings.verify")}
        </Button>
      </form>
    </Card>
  );
}
