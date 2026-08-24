import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type Booking } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, Input, Spinner, formatDateTime, formatFee } from "../components/ui";

export function StaffQueue() {
  const { t } = useTranslation();
  const { isStaff } = useAuth();
  const qc = useQueryClient();
  const [error, setError] = useState("");
  const [conditioning, setConditioning] = useState("");
  const { data, isLoading } = useQuery({ queryKey: ["pending"], queryFn: api.pendingBookings, enabled: isStaff });

  const decide = useMutation({
    mutationFn: ({ id, action }: { id: string; action: "approve" | "deny" }) =>
      action === "approve" ? api.approve(id) : api.deny(id),
    onSuccess: () => { setError(""); void qc.invalidateQueries({ queryKey: ["pending"] }); },
    onError: (e: Error) => setError(e.message),
  });

  if (!isStaff) return <Card className="p-8 text-center text-slate-500">{t("common.staffOnly")}</Card>;
  if (isLoading) return <Spinner />;

  return (
    <div>
      <h2 className="mb-1 text-2xl font-semibold">{t("staffQueue.title")}</h2>
      <p className="mb-6 text-slate-500">{t("staffQueue.subtitle")}</p>

      {error && <p role="alert" className="mb-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</p>}

      {!data?.length ? (
        <Card className="p-8 text-center text-slate-500">{t("staffQueue.empty")}</Card>
      ) : (
        <div className="space-y-3">
          {data.map((b) => (
            <Card key={b.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
              <div>
                <p className="font-semibold">{b.facility?.name ?? "Facility"}</p>
                <p className="text-sm text-slate-500">
                  {formatDateTime(b.startsAt)} – {formatDateTime(b.endsAt)} · {b.attendance} {t("staffQueue.people")} · {formatFee(b.feeCents)}
                </p>
                <p className="text-sm text-slate-500">
                  {b.user?.name || b.user?.email} · {b.purpose || t("staffQueue.noPurpose")}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <Button variant="outline" disabled={decide.isPending} onClick={() => decide.mutate({ id: b.id, action: "deny" })}>{t("staffQueue.deny")}</Button>
                <Button variant="outline" onClick={() => setConditioning(conditioning === b.id ? "" : b.id)}>
                  {t("staffQueue.withConditions")}
                </Button>
                <Button disabled={decide.isPending} onClick={() => decide.mutate({ id: b.id, action: "approve" })}>{t("staffQueue.approve")}</Button>
              </div>

              {conditioning === b.id && (
                <div className="w-full border-t border-slate-100 pt-4">
                  <ConditionForm booking={b} onDone={() => setConditioning("")} />
                </div>
              )}
            </Card>
          ))}
        </div>
      )}

      <AwaitingResident />
    </div>
  );
}

// ConditionForm is §4.8's "add conditions to" action. All three fields are
// optional individually but at least one is required — approving with nothing
// attached would park the booking short of confirmed while telling the resident
// nothing to do about it, so the server rejects it and the button stays disabled.
function ConditionForm({ booking, onDone }: { booking: Booking; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [terms, setTerms] = useState("");
  const [fee, setFee] = useState("");
  const [doc, setDoc] = useState("");
  const [error, setError] = useState("");

  const feeCents = fee ? Math.round(Number(fee) * 100) : 0;
  const nothingImposed = !terms.trim() && feeCents <= 0 && !doc.trim();

  const save = useMutation({
    mutationFn: () => api.approveWithConditions(booking.id, { terms, additionalFeeCents: feeCents, documentLabel: doc }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["pending"] });
      void qc.invalidateQueries({ queryKey: ["awaitingResident"] });
      onDone();
    },
    onError: (e: Error) => setError(e.message),
  });

  return (
    <div className="space-y-3">
      <label className="block text-sm">
        <span className="mb-1 block text-slate-500">{t("staffQueue.conditionTerms")}</span>
        <Input value={terms} onChange={(e) => setTerms(e.target.value)} placeholder={t("staffQueue.conditionTermsPlaceholder")} />
      </label>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block text-sm">
          <span className="mb-1 block text-slate-500">{t("staffQueue.conditionFee")}</span>
          <Input type="number" min={0} step={0.01} value={fee} onChange={(e) => setFee(e.target.value)} placeholder="0.00" />
        </label>
        <label className="block text-sm">
          <span className="mb-1 block text-slate-500">{t("staffQueue.conditionDocument")}</span>
          <Input value={doc} onChange={(e) => setDoc(e.target.value)} placeholder={t("staffQueue.conditionDocumentPlaceholder")} />
        </label>
      </div>
      <p className="text-xs text-slate-500">{t("staffQueue.conditionHint")}</p>
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      <div className="flex gap-2">
        <Button disabled={nothingImposed || save.isPending} onClick={() => { setError(""); save.mutate(); }}>
          {save.isPending ? t("staffQueue.saving") : t("staffQueue.approveWithConditions")}
        </Button>
        <Button variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      </div>
    </div>
  );
}

// AwaitingResident answers "which conditional approvals are stuck, and on what".
// Without it a conditional approval is fire-and-forget: staff impose terms and
// never learn whether anyone acted on them.
function AwaitingResident() {
  const { t } = useTranslation();
  const { data } = useQuery({ queryKey: ["awaitingResident"], queryFn: api.awaitingResident });

  if (!data?.length) return null;

  return (
    <div className="mt-10">
      <h3 className="mb-1 text-lg font-semibold">{t("staffQueue.awaitingTitle")}</h3>
      <p className="mb-3 text-sm text-slate-500">{t("staffQueue.awaitingHint")}</p>
      <div className="space-y-2">
        {data.map((b) => (
          <Card key={b.id} className="p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="font-medium">{b.facility?.name ?? "Facility"}</p>
                <p className="text-sm text-slate-500">
                  {formatDateTime(b.startsAt)} · {b.user?.name || b.user?.email}
                </p>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {b.outstanding.acceptTerms && <Badge tone="amber">{t("staffQueue.waitingTerms")}</Badge>}
                {b.outstanding.payCents > 0 && <Badge tone="amber">{t("staffQueue.waitingPayment", { amount: formatFee(b.outstanding.payCents) })}</Badge>}
                {b.outstanding.uploadLabel && <Badge tone="amber">{t("staffQueue.waitingDocument", { label: b.outstanding.uploadLabel })}</Badge>}
              </div>
            </div>
            {b.condition?.terms && <p className="mt-2 text-sm text-slate-600">{b.condition.terms}</p>}
          </Card>
        ))}
      </div>
    </div>
  );
}
