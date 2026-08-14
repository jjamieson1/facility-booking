import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Button, Card, Spinner, formatDateTime, formatFee } from "../components/ui";

export function StaffQueue() {
  const { t } = useTranslation();
  const { isStaff } = useAuth();
  const qc = useQueryClient();
  const [error, setError] = useState("");
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
                <Button disabled={decide.isPending} onClick={() => decide.mutate({ id: b.id, action: "approve" })}>{t("staffQueue.approve")}</Button>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
