import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type Facility, type FacilityInput } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, Input, Spinner, formatFee } from "../components/ui";

export function StaffFacilities() {
  const { t } = useTranslation();
  const { isStaff } = useAuth();
  const [creating, setCreating] = useState(false);
  const { data, isLoading } = useQuery({ queryKey: ["facilities", {}], queryFn: () => api.listFacilities(), enabled: isStaff });

  if (!isStaff) return <Card className="p-8 text-center text-slate-500">{t("common.staffOnly")}</Card>;
  if (isLoading) return <Spinner />;

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-semibold">{t("manage.title")}</h2>
          <p className="text-slate-500">{t("manage.subtitle")}</p>
        </div>
        <Button onClick={() => setCreating((v) => !v)}>{creating ? t("common.cancel") : t("manage.newFacility")}</Button>
      </div>

      {creating && (
        <Card className="mb-6 p-5">
          <h3 className="mb-4 font-semibold">{t("manage.newFacilityTitle")}</h3>
          <FacilityForm onDone={() => setCreating(false)} />
        </Card>
      )}

      <div className="space-y-3">
        {data?.map((f) => <FacilityRow key={f.id} facility={f} />)}
      </div>
    </div>
  );
}

function FacilityRow({ facility }: { facility: Facility }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [panel, setPanel] = useState<"none" | "edit" | "blackouts" | "translations">("none");
  const del = useMutation({
    mutationFn: () => api.deleteFacility(facility.id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["facilities"] }),
  });

  return (
    <Card className="p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-semibold">{facility.name}</p>
          <p className="text-sm text-slate-500">
            {t("list.upTo", { count: facility.capacity })} · {formatFee(facility.feeCents)} · {facility.location}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {facility.requiresApproval && <Badge tone="amber">{t("manage.approval")}</Badge>}
          <Button variant="outline" onClick={() => setPanel(panel === "edit" ? "none" : "edit")}>{t("manage.edit")}</Button>
          <Button variant="outline" onClick={() => setPanel(panel === "blackouts" ? "none" : "blackouts")}>{t("manage.blackouts")}</Button>
          <Button variant="outline" onClick={() => setPanel(panel === "translations" ? "none" : "translations")}>{t("manage.translations")}</Button>
          <Button variant="ghost" onClick={() => { if (confirm(t("manage.deleteConfirm", { name: facility.name }))) del.mutate(); }}>{t("manage.delete")}</Button>
        </div>
      </div>

      {panel === "edit" && (
        <div className="mt-4 border-t border-slate-100 pt-4">
          <FacilityForm facility={facility} onDone={() => setPanel("none")} />
        </div>
      )}
      {panel === "blackouts" && (
        <div className="mt-4 border-t border-slate-100 pt-4">
          <BlackoutManager facilityId={facility.id} />
        </div>
      )}
      {panel === "translations" && (
        <div className="mt-4 border-t border-slate-100 pt-4">
          <TranslationEditor facilityId={facility.id} />
        </div>
      )}
    </Card>
  );
}

function toInput(f?: Facility): FacilityInput {
  return {
    name: f?.name ?? "",
    description: f?.description ?? "",
    capacity: f?.capacity ?? 0,
    feeCents: f?.feeCents ?? 0,
    nonResidentFeeCents: f?.nonResidentFeeCents ?? 0,
    depositCents: f?.depositCents ?? 0,
    location: f?.location ?? "",
    imageUrl: f?.imageUrl ?? "",
    latitude: f?.latitude ?? 0,
    longitude: f?.longitude ?? 0,
    requiresApproval: f?.requiresApproval ?? false,
    requiresWaiver: f?.requiresWaiver ?? false,
    minMinutes: f?.minMinutes ?? 60,
    maxMinutes: f?.maxMinutes ?? 240,
    bufferMinutes: f?.bufferMinutes ?? 0,
    stepFreeAccess: f?.stepFreeAccess ?? false,
    accessibleWashroom: f?.accessibleWashroom ?? false,
    beforeInstructions: f?.beforeInstructions ?? "",
    afterInstructions: f?.afterInstructions ?? "",
  };
}

function FacilityForm({ facility, onDone }: { facility?: Facility; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [form, setForm] = useState<FacilityInput>(() => toInput(facility));
  const [error, setError] = useState("");
  const set = <K extends keyof FacilityInput>(k: K, v: FacilityInput[K]) => setForm((f) => ({ ...f, [k]: v }));

  const save = useMutation({
    mutationFn: () => (facility ? api.updateFacility(facility.id, form) : api.createFacility(form)),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["facilities"] }); onDone(); },
    onError: (e: Error) => setError(e.message),
  });

  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t("manage.name")}><Input value={form.name} onChange={(e) => set("name", e.target.value)} /></Field>
        <Field label={t("manage.location")}><Input value={form.location} onChange={(e) => set("location", e.target.value)} /></Field>
        <Field label={t("manage.capacity")}><Input type="number" min={0} value={form.capacity} onChange={(e) => set("capacity", Number(e.target.value))} /></Field>
        <Field label={t("manage.imageUrl")}><Input value={form.imageUrl} onChange={(e) => set("imageUrl", e.target.value)} /></Field>
        <Field label={t("manage.residentFee")}><Input type="number" min={0} step={0.01} value={form.feeCents / 100} onChange={(e) => set("feeCents", Math.round(Number(e.target.value) * 100))} /></Field>
        <Field label={t("manage.nonResidentFee")}><Input type="number" min={0} step={0.01} value={form.nonResidentFeeCents / 100} onChange={(e) => set("nonResidentFeeCents", Math.round(Number(e.target.value) * 100))} /></Field>
        <Field label={t("manage.deposit")}><Input type="number" min={0} step={0.01} value={form.depositCents / 100} onChange={(e) => set("depositCents", Math.round(Number(e.target.value) * 100))} /></Field>
        <Field label={t("manage.minMinutes")}><Input type="number" min={0} step={30} value={form.minMinutes} onChange={(e) => set("minMinutes", Number(e.target.value))} /></Field>
        <Field label={t("manage.maxMinutes")}><Input type="number" min={0} step={30} value={form.maxMinutes} onChange={(e) => set("maxMinutes", Number(e.target.value))} /></Field>
        <Field label={t("manage.bufferMinutes")}><Input type="number" min={0} step={15} value={form.bufferMinutes} onChange={(e) => set("bufferMinutes", Number(e.target.value))} /></Field>
        <Field label={t("manage.latitude")}><Input type="number" step="any" value={form.latitude} onChange={(e) => set("latitude", Number(e.target.value))} /></Field>
        <Field label={t("manage.longitude")}><Input type="number" step="any" value={form.longitude} onChange={(e) => set("longitude", Number(e.target.value))} /></Field>
      </div>
      <Field label={t("manage.description")}><Input value={form.description} onChange={(e) => set("description", e.target.value)} /></Field>
      <div className="flex flex-wrap gap-6 text-sm text-slate-600">
        <label className="flex items-center gap-2"><input type="checkbox" className="h-4 w-4" checked={form.requiresApproval} onChange={(e) => set("requiresApproval", e.target.checked)} /> {t("manage.requiresApproval")}</label>
        <label className="flex items-center gap-2"><input type="checkbox" className="h-4 w-4" checked={form.requiresWaiver} onChange={(e) => set("requiresWaiver", e.target.checked)} /> {t("manage.requiresWaiver")}</label>
        <label className="flex items-center gap-2"><input type="checkbox" className="h-4 w-4" checked={form.stepFreeAccess} onChange={(e) => set("stepFreeAccess", e.target.checked)} /> {t("manage.stepFree")}</label>
        <label className="flex items-center gap-2"><input type="checkbox" className="h-4 w-4" checked={form.accessibleWashroom} onChange={(e) => set("accessibleWashroom", e.target.checked)} /> {t("manage.accessibleWashroom")}</label>
      </div>
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      <div className="flex gap-2">
        <Button disabled={save.isPending || !form.name} onClick={() => { setError(""); save.mutate(); }}>
          {save.isPending ? t("manage.saving") : facility ? t("manage.saveChanges") : t("manage.createFacility")}
        </Button>
        <Button variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      </div>
    </div>
  );
}

function BlackoutManager({ facilityId }: { facilityId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const key = ["blackouts", facilityId];
  const { data, isLoading } = useQuery({ queryKey: key, queryFn: () => api.listBlackouts(facilityId) });
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");

  const add = useMutation({
    mutationFn: () => {
      // Block whole days: [from 00:00, dayAfter(to) 00:00).
      const start = new Date(`${from}T00:00:00`);
      const end = new Date(`${to}T00:00:00`);
      end.setDate(end.getDate() + 1);
      return api.addBlackout(facilityId, { start: start.toISOString(), end: end.toISOString(), reason });
    },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: key }); setFrom(""); setTo(""); setReason(""); },
    onError: (e: Error) => setError(e.message),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.removeBlackout(facilityId, id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key }),
  });

  const fmt = (iso: string) => new Date(iso).toLocaleDateString();

  return (
    <div className="space-y-4">
      <h4 className="font-medium">{t("manage.blackoutTitle")}</h4>
      {isLoading ? (
        <Spinner />
      ) : data?.length ? (
        <ul className="space-y-1 text-sm">
          {data.map((b) => (
            <li key={b.id} className="flex items-center justify-between rounded-lg bg-slate-50 px-3 py-2">
              <span>{fmt(b.startsAt)} – {fmt(b.endsAt)}{b.reason ? ` · ${b.reason}` : ""}</span>
              <button className="text-red-600 hover:underline" onClick={() => remove.mutate(b.id)}>{t("common.remove")}</button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-slate-500">{t("manage.noBlackouts")}</p>
      )}

      <div className="flex flex-wrap items-end gap-3">
        <Field label={t("list.from")}><Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} /></Field>
        <Field label={t("list.to")}><Input type="date" value={to} onChange={(e) => setTo(e.target.value)} /></Field>
        <Field label={t("manage.reason")}><Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder={t("manage.reasonPlaceholder")} /></Field>
        <Button className="mb-0.5" disabled={!from || !to || add.isPending} onClick={() => { setError(""); add.mutate(); }}>{t("manage.blockDates")}</Button>
      </div>
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-slate-500">{label}</span>
      {children}
    </label>
  );
}

// TranslationEditor edits one facility's text per language (§4.11).
//
// Separate from the main form on purpose: only some fields are translatable.
// Capacity, fees and the street address are the same in both languages, and
// putting them behind a language tab would invite someone to "translate" an
// address or enter a second, divergent capacity.
function TranslationEditor({ facilityId }: { facilityId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [lang, setLang] = useState<"en" | "fr">("fr");
  const [draft, setDraft] = useState<Record<string, string> | null>(null);
  const [error, setError] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: ["facilityTranslations", facilityId],
    queryFn: () => api.facilityTranslations(facilityId),
  });

  const stored = data?.find((x) => x.language === lang);
  const value = (field: keyof typeof fields) =>
    draft?.[field] ?? (stored ? (stored[field] as string) ?? "" : "");

  const fields = {
    name: t("manage.name"),
    description: t("manage.description"),
    beforeInstructions: t("manage.beforeInstructions"),
    afterInstructions: t("manage.afterInstructions"),
  };

  const save = useMutation({
    mutationFn: () =>
      api.saveFacilityTranslation(facilityId, {
        language: lang,
        name: value("name"),
        description: value("description"),
        beforeInstructions: value("beforeInstructions"),
        afterInstructions: value("afterInstructions"),
      }),
    onSuccess: (rows) => {
      setDraft(null);
      setError("");
      qc.setQueryData(["facilityTranslations", facilityId], rows);
      // The directory and detail pages render this text.
      void qc.invalidateQueries({ queryKey: ["facilities"] });
    },
    onError: (e: Error) => setError(e.message),
  });

  if (isLoading) return <Spinner />;

  return (
    <div className="space-y-3">
      <div>
        <h4 className="font-semibold">{t("manage.translationsTitle")}</h4>
        <p className="text-sm text-slate-500">{t("manage.translationsHint")}</p>
      </div>

      <div role="group" aria-label={t("a11y.language")} className="flex gap-1 text-sm">
        {(["en", "fr"] as const).map((l) => (
          <button
            key={l}
            type="button"
            onClick={() => { setLang(l); setDraft(null); }}
            aria-pressed={lang === l}
            className={`rounded-lg px-3 py-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 ${
              lang === l ? "bg-brand-500 text-white" : "bg-white text-slate-600 hover:bg-slate-100"
            }`}
          >
            {l.toUpperCase()}
            {/* A language with no name yet has nothing stored at all. */}
            {l !== "en" && !data?.find((x) => x.language === l)?.name && " •"}
          </button>
        ))}
      </div>

      {Object.entries(fields).map(([field, label]) => (
        <Field key={field} label={label}>
          <Input
            value={value(field as keyof typeof fields)}
            onChange={(e) => setDraft((d) => ({ ...(d ?? {}), [field]: e.target.value }))}
          />
        </Field>
      ))}

      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      <div className="flex gap-2">
        <Button disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? t("manage.saving") : t("manage.saveChanges")}
        </Button>
      </div>
    </div>
  );
}
