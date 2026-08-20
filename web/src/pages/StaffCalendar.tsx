import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type CalendarKind, type CalendarModule } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, Input, Spinner } from "../components/ui";

// StaffCalendar lets the municipality pick which calendar module the app runs
// on (§4.6). The list of types comes from the API's module registry rather than
// being hardcoded here, so a new module appears in this form as soon as it is
// registered on the server.
export function StaffCalendar() {
  const { t } = useTranslation();
  const { isAdmin } = useAuth();
  const qc = useQueryClient();
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [kind, setKind] = useState<CalendarKind | null>(null);
  const [config, setConfig] = useState<Record<string, string>>({});

  const { data, isLoading } = useQuery({ queryKey: ["calendarSettings"], queryFn: api.calendarSettings });

  // Seed the form from the saved settings once they arrive.
  useEffect(() => {
    if (data && kind === null) {
      setKind(data.settings.selected);
      setConfig(data.settings.config ?? {});
    }
  }, [data, kind]);

  const save = useMutation({
    mutationFn: () => api.setCalendarSettings(kind as CalendarKind, config),
    onSuccess: (res) => {
      setError("");
      setNotice(t("calendarSettings.saved", { name: moduleName(res.modules, res.settings.selected) }));
      qc.setQueryData(["calendarSettings"], res);
    },
    onError: (e: Error) => { setNotice(""); setError(e.message); },
  });

  if (isLoading || !data || kind === null) return <Spinner />;

  const selected = data.modules.find((m) => m.kind === kind);
  const dirty = kind !== data.settings.selected || !sameConfig(config, data.settings.config ?? {});
  const missingRequired = (selected?.fields ?? []).some((f) => f.required && !(config[f.key] ?? "").trim());

  // Switching module resets the config to whatever was saved for that module —
  // fields are module-specific, so carrying values across would post keys the
  // server rejects.
  const pick = (next: CalendarKind) => {
    setKind(next);
    setConfig(next === data.settings.selected ? data.settings.config ?? {} : {});
    setNotice("");
    setError("");
  };

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-semibold uppercase tracking-wide text-brand-600">{t("calendarSettings.eyebrow")}</p>
        <h2 className="text-3xl font-bold tracking-tight">{t("calendarSettings.title")}</h2>
        <p className="mt-1 text-slate-500">{t("calendarSettings.subtitle")}</p>
      </div>

      {error && <p role="alert" className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</p>}
      {notice && <p role="status" className="rounded-lg bg-green-50 p-3 text-sm text-green-700">{notice}</p>}

      {/* What the app is actually doing right now, which is not always what was
          chosen — a two-way module can be selected before it is built. */}
      <Card className="p-5">
        <h3 className="mb-1 font-semibold">{t("calendarSettings.currentTitle")}</h3>
        <p className="text-sm text-slate-500">
          {t("calendarSettings.running", { name: moduleName(data.modules, data.settings.effective) })}
        </p>
        {data.settings.fallbackNotes && (
          <p role="status" className="mt-3 rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
            {data.settings.fallbackNotes}
          </p>
        )}
      </Card>

      <Card className="p-5">
        <form
          onSubmit={(e) => { e.preventDefault(); if (dirty && !missingRequired) save.mutate(); }}
        >
          <fieldset disabled={!isAdmin}>
            <legend className="mb-1 font-semibold">{t("calendarSettings.chooseTitle")}</legend>
            <p className="mb-4 text-sm text-slate-500">
              {isAdmin ? t("calendarSettings.chooseHint") : t("calendarSettings.readOnly")}
            </p>

            <div className="space-y-3">
              {data.modules.map((m) => (
                <ModuleOption
                  key={m.kind}
                  module={m}
                  checked={kind === m.kind}
                  onSelect={() => pick(m.kind)}
                />
              ))}
            </div>

            {(selected?.fields.length ?? 0) > 0 && (
              <div className="mt-5 border-t border-slate-100 pt-5">
                <h4 className="mb-3 text-sm font-semibold">{t("calendarSettings.configTitle", { name: selected?.name })}</h4>
                <div className="grid gap-4 sm:grid-cols-2">
                  {selected?.fields.map((f) => (
                    <label key={f.key} className="text-sm">
                      <span className="mb-1 block text-slate-500">
                        {f.label}
                        {f.required && <span className="ml-1 text-red-600" aria-hidden="true">*</span>}
                        {f.required && <span className="sr-only">{t("calendarSettings.required")}</span>}
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
                    {t("calendarSettings.secretHint")}{" "}
                    <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-700">{selected.secretEnv}</code>
                  </p>
                )}
              </div>
            )}

            {isAdmin && (
              <div className="mt-5 flex items-center gap-3">
                <Button type="submit" disabled={!dirty || missingRequired || save.isPending}>
                  {save.isPending ? t("calendarSettings.saving") : t("calendarSettings.save")}
                </Button>
                {dirty && <span className="text-sm text-slate-500">{t("calendarSettings.unsaved")}</span>}
              </div>
            )}
          </fieldset>
        </form>
      </Card>
    </div>
  );
}

// ModuleOption is one selectable calendar type: a radio with the module's
// capabilities spelled out, so the choice is made on informed terms.
function ModuleOption({ module: m, checked, onSelect }: { module: CalendarModule; checked: boolean; onSelect: () => void }) {
  const { t } = useTranslation();
  return (
    <label
      className={`flex cursor-pointer gap-3 rounded-lg border p-4 focus-within:ring-2 focus-within:ring-brand-500 ${
        checked ? "border-brand-500 bg-brand-50" : "border-slate-200 hover:bg-slate-50"
      }`}
    >
      <input
        type="radio"
        name="calendarModule"
        value={m.kind}
        checked={checked}
        onChange={onSelect}
        className="mt-1 h-4 w-4 accent-brand-600 focus-visible:outline-none"
      />
      <span className="flex-1">
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{m.name}</span>
          <Badge tone={m.twoWay ? "brand" : "slate"}>
            {m.twoWay ? t("calendarSettings.twoWay") : t("calendarSettings.oneWay")}
          </Badge>
          {!m.available && <Badge tone="amber">{t("calendarSettings.planned")}</Badge>}
        </span>
        <span className="mt-1 block text-sm text-slate-500">{m.summary}</span>
        {!m.available && (
          <span className="mt-1 block text-sm text-amber-700">{t("calendarSettings.plannedHint")}</span>
        )}
      </span>
    </label>
  );
}

function moduleName(modules: CalendarModule[], kind: CalendarKind): string {
  return modules.find((m) => m.kind === kind)?.name ?? kind;
}

function sameConfig(a: Record<string, string>, b: Record<string, string>): boolean {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  for (const k of keys) {
    if ((a[k] ?? "").trim() !== (b[k] ?? "").trim()) return false;
  }
  return true;
}
