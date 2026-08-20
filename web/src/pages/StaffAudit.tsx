import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Card, Spinner, formatDateTime } from "../components/ui";

export function StaffAudit() {
  const { t } = useTranslation();
  const { isStaff } = useAuth();
  const { data, isLoading, error } = useQuery({ queryKey: ["audit"], queryFn: api.auditLog, enabled: isStaff });

  if (!isStaff) return <Card className="p-8 text-center text-slate-500">{t("common.staffOnly")}</Card>;
  if (isLoading) return <Spinner />;

  return (
    <div>
      <h2 className="mb-1 text-2xl font-semibold">{t("audit.title")}</h2>
      <p className="mb-6 text-slate-500">{t("audit.subtitle")}</p>

      {error ? (
        <Card className="p-8 text-center text-red-600">{t("audit.unavailable")}</Card>
      ) : !data?.enabled ? (
        <Card className="p-8 text-center text-slate-500">{t("audit.disabled")}</Card>
      ) : !data.entries.length ? (
        <Card className="p-8 text-center text-slate-500">{t("audit.empty")}</Card>
      ) : (
        <Card className="overflow-x-auto p-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-100 text-left text-slate-500">
                <th className="px-4 py-3">{t("audit.when")}</th>
                <th className="px-4 py-3">{t("audit.action")}</th>
                <th className="px-4 py-3">{t("audit.actor")}</th>
                <th className="px-4 py-3">{t("audit.target")}</th>
              </tr>
            </thead>
            <tbody>
              {data.entries.map((e) => (
                <tr key={e.index} className="border-b border-slate-50 last:border-0">
                  <td className="whitespace-nowrap px-4 py-3 text-slate-600">{formatDateTime(e.timestamp)}</td>
                  <td className="px-4 py-3"><Badge tone="slate">{e.action || e.message}</Badge></td>
                  <td className="px-4 py-3 text-slate-600">{e.actorEmail || e.actorId || "—"}</td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-500">{e.targetType}:{e.targetId ? e.targetId.slice(0, 8) : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
    </div>
  );
}
