import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type PaymentKind, type PaymentModule } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, Input, Spinner, formatDateTime, formatFee } from "../components/ui";

// StaffPaymentSettings lets an admin choose which gateway the municipality takes
// money through (§4.7). The list comes from the API's module registry, so a new
// provider appears here as soon as it is registered server-side.
export function StaffPaymentSettings() {
  const { t } = useTranslation();
  const { isAdmin } = useAuth();
  const qc = useQueryClient();
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [kind, setKind] = useState<PaymentKind | null>(null);
  const [config, setConfig] = useState<Record<string, string>>({});

  const { data, isLoading } = useQuery({ queryKey: ["paymentSettings"], queryFn: api.paymentSettings });

  useEffect(() => {
    if (data && kind === null) {
      setKind(data.settings.selected);
      setConfig(data.settings.config ?? {});
    }
  }, [data, kind]);

  const save = useMutation({
    mutationFn: () => api.setPaymentSettings(kind as PaymentKind, config),
    onSuccess: (res) => {
      setError("");
      setNotice(t("paymentSettings.saved", { name: nameOf(res.modules, res.settings.selected) }));
      qc.setQueryData(["paymentSettings"], res);
    },
    onError: (e: Error) => { setNotice(""); setError(e.message); },
  });

  if (isLoading || !data || kind === null) return <Spinner />;

  const selected = data.modules.find((m) => m.kind === kind);
  const dirty = kind !== data.settings.selected || !sameConfig(config, data.settings.config ?? {});
  const missing = (selected?.fields ?? []).some((f) => f.required && !(config[f.key] ?? "").trim());

  const pick = (next: PaymentKind) => {
    setKind(next);
    setConfig(next === data.settings.selected ? data.settings.config ?? {} : {});
    setNotice(""); setError("");
  };

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-semibold uppercase tracking-wide text-brand-600">{t("paymentSettings.eyebrow")}</p>
        <h2 className="text-3xl font-bold tracking-tight">{t("paymentSettings.title")}</h2>
        <p className="mt-1 text-slate-500">{t("paymentSettings.subtitle")}</p>
      </div>

      {error && <p role="alert" className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</p>}
      {notice && <p role="status" className="rounded-lg bg-green-50 p-3 text-sm text-green-700">{notice}</p>}

      {/* What money is actually flowing through right now, which is not always
          what was chosen — an unbuilt module falls back to the simulator. */}
      <Card className="p-5">
        <h3 className="mb-1 font-semibold">{t("paymentSettings.currentTitle")}</h3>
        <p className="text-sm text-slate-500">
          {t("paymentSettings.running", { name: nameOf(data.modules, data.settings.effective) })}
        </p>
        {data.settings.fallbackNotes && (
          <p role="alert" className="mt-3 rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
            {data.settings.fallbackNotes}
          </p>
        )}
      </Card>

      <RefundObligations />

      <Card className="p-5">
        <form onSubmit={(e) => { e.preventDefault(); if (dirty && !missing) save.mutate(); }}>
          <fieldset disabled={!isAdmin}>
            <legend className="mb-1 font-semibold">{t("paymentSettings.chooseTitle")}</legend>
            <p className="mb-4 text-sm text-slate-500">
              {isAdmin ? t("paymentSettings.chooseHint") : t("paymentSettings.readOnly")}
            </p>

            <div className="space-y-3">
              {data.modules.map((m) => (
                <ModuleOption key={m.kind} module={m} checked={kind === m.kind} onSelect={() => pick(m.kind)} />
              ))}
            </div>

            {(selected?.fields ?? []).length > 0 && (
              <div className="mt-5 border-t border-slate-100 pt-5">
                <h4 className="mb-3 text-sm font-semibold">{t("paymentSettings.configTitle", { name: selected?.name })}</h4>
                <div className="grid gap-4 sm:grid-cols-2">
                  {(selected?.fields ?? []).map((f) => (
                    <label key={f.key} className="text-sm">
                      <span className="mb-1 block text-slate-500">
                        {f.label}
                        {f.required && <span className="ml-1 text-red-600" aria-hidden="true">*</span>}
                        {f.required && <span className="sr-only">{t("paymentSettings.required")}</span>}
                      </span>
                      <Input
                        value={config[f.key] ?? ""}
                        placeholder={f.placeholder}
                        required={f.required}
                        onChange={(e) => setConfig((c) => ({ ...c, [f.key]: e.target.value }))}
                      />
                    </label>
                  ))}
                </div>
                {selected?.secretEnv && (
                  <p className="mt-3 text-sm text-slate-500">
                    {t("paymentSettings.secretHint")}{" "}
                    <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-700">{selected.secretEnv}</code>
                  </p>
                )}
              </div>
            )}

            {isAdmin && (
              <div className="mt-5 flex items-center gap-3">
                <Button type="submit" disabled={!dirty || missing || save.isPending}>
                  {save.isPending ? t("paymentSettings.saving") : t("paymentSettings.save")}
                </Button>
                {dirty && <span className="text-sm text-slate-500">{t("paymentSettings.unsaved")}</span>}
              </div>
            )}
          </fieldset>
        </form>
      </Card>
    </div>
  );
}

// RefundObligations is the queue of money owed but not yet returned.
//
// It exists because a hosted gateway need not accept refund instructions from
// the billing application — C2's partner API has no refund endpoint at all, so
// an operator has to issue it inside C2. The cancellation still happened and the
// resident is still owed, so the debt is listed here with the reference an
// operator needs to find it. Rendered even when empty is *not* the choice: an
// empty queue is the normal state and a permanent empty card would train staff
// to ignore this area.
function RefundObligations() {
  const { t } = useTranslation();
  const { data, isLoading } = useQuery({ queryKey: ["refundObligations"], queryFn: () => api.refundObligations("owed") });

  if (isLoading || !data?.length) return null;

  return (
    <Card className="p-5">
      <h3 className="mb-1 font-semibold">{t("paymentSettings.owedTitle")}</h3>
      <p className="mb-4 text-sm text-slate-600">{t("paymentSettings.owedHint")}</p>
      <ul className="space-y-2">
        {data.map((o) => (
          <li key={o.id} className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="font-medium text-amber-900">
                {formatFee(o.amountCents)} · {o.booking?.facility?.name ?? t("paymentSettings.owedBooking")}
              </span>
              <Badge tone="amber">{t("paymentSettings.owedBadge")}</Badge>
            </div>
            {o.reason && <p className="mt-1 text-amber-800">{o.reason}</p>}
            <p className="mt-1 text-xs text-amber-700">
              {t("paymentSettings.owedRef", { provider: o.provider, ref: o.providerRef })}
              {" · "}
              {formatDateTime(o.createdAt)}
            </p>
          </li>
        ))}
      </ul>
    </Card>
  );
}

// ModuleOption shows a gateway with its capabilities spelled out — whether it
// refunds in-app and whether it can hold a deposit are the two questions that
// decide whether a municipality can use it.
function ModuleOption({ module: m, checked, onSelect }: { module: PaymentModule; checked: boolean; onSelect: () => void }) {
  const { t } = useTranslation();
  return (
    <label
      className={`flex cursor-pointer gap-3 rounded-lg border p-4 focus-within:ring-2 focus-within:ring-brand-500 ${
        checked ? "border-brand-500 bg-brand-50" : "border-slate-200 hover:bg-slate-50"
      }`}
    >
      <input
        type="radio"
        name="paymentModule"
        value={m.kind}
        checked={checked}
        onChange={onSelect}
        className="mt-1 h-4 w-4 accent-brand-600 focus-visible:outline-none"
      />
      <span className="flex-1">
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{m.name}</span>
          {!m.available && <Badge tone="amber">{t("paymentSettings.planned")}</Badge>}
          <Badge tone={m.supportsRefund ? "green" : "slate"}>
            {m.supportsRefund ? t("paymentSettings.refunds") : t("paymentSettings.noRefunds")}
          </Badge>
          <Badge tone={m.supportsHold ? "green" : "slate"}>
            {m.supportsHold ? t("paymentSettings.holds") : t("paymentSettings.noHolds")}
          </Badge>
        </span>
        <span className="mt-1 block text-sm text-slate-500">{m.summary}</span>
        {!m.available && (
          <span className="mt-1 block text-sm text-amber-700">{t("paymentSettings.plannedHint")}</span>
        )}
      </span>
    </label>
  );
}

function nameOf(modules: PaymentModule[], kind: PaymentKind): string {
  return modules.find((m) => m.kind === kind)?.name ?? kind;
}

function sameConfig(a: Record<string, string>, b: Record<string, string>): boolean {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  for (const k of keys) if ((a[k] ?? "").trim() !== (b[k] ?? "").trim()) return false;
  return true;
}
