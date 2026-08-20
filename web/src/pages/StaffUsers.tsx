import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type Role, type RoleGrant, type User } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Badge, Button, Card, Input, Spinner } from "../components/ui";

export function StaffUsers() {
  const { t } = useTranslation();
  const { isAdmin, user } = useAuth();
  const qc = useQueryClient();
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const { data, isLoading } = useQuery({ queryKey: ["adminUsers"], queryFn: api.adminUsers, enabled: isAdmin });

  const refresh = () => qc.invalidateQueries({ queryKey: ["adminUsers"] });
  const onError = (e: Error) => { setNotice(""); setError(e.message); };

  const setRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: Role }) => api.setUserRole(id, role),
    onSuccess: () => { setError(""); void refresh(); },
    onError,
  });
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeInvite(id),
    onSuccess: () => { setError(""); void refresh(); },
    onError,
  });

  if (!isAdmin) return <Card className="p-8 text-center text-slate-500">{t("users.adminOnly")}</Card>;
  if (isLoading || !data) return <Spinner />;

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-semibold uppercase tracking-wide text-brand-600">{t("users.eyebrow")}</p>
        <h2 className="text-3xl font-bold tracking-tight">{t("users.title")}</h2>
        <p className="mt-1 text-slate-500">{t("users.subtitle")}</p>
      </div>

      {error && <p role="alert" className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</p>}
      {notice && <p role="status" className="rounded-lg bg-green-50 p-3 text-sm text-green-700">{notice}</p>}

      <InviteForm
        onDone={(msg) => { setError(""); setNotice(msg); void refresh(); }}
        onError={onError}
      />

      {/* Current staff + admins */}
      <Card className="overflow-x-auto p-0">
        <div className="border-b border-slate-100 px-5 py-4">
          <h3 className="font-semibold">{t("users.team")}</h3>
          <p className="text-sm text-slate-500">{t("users.teamHint")}</p>
        </div>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-100 text-left text-slate-500">
              <th className="px-5 py-3 font-medium">{t("users.name")}</th>
              <th className="px-5 py-3 font-medium">{t("users.email")}</th>
              <th className="px-5 py-3 font-medium">{t("users.role")}</th>
              <th className="px-5 py-3 text-right font-medium">{t("users.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {data.users.map((u) => (
              <UserRow
                key={u.id}
                u={u}
                isSelf={u.id === user?.id}
                busy={setRole.isPending}
                onSetRole={(role) => { setError(""); setNotice(""); setRole.mutate({ id: u.id, role }); }}
              />
            ))}
          </tbody>
        </table>
      </Card>

      {/* Pending invites */}
      {data.invites.length > 0 && (
        <Card className="overflow-x-auto p-0">
          <div className="border-b border-slate-100 px-5 py-4">
            <h3 className="font-semibold">{t("users.pending")}</h3>
            <p className="text-sm text-slate-500">{t("users.pendingHint")}</p>
          </div>
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-100 text-left text-slate-500">
                <th className="px-5 py-3 font-medium">{t("users.email")}</th>
                <th className="px-5 py-3 font-medium">{t("users.role")}</th>
                <th className="px-5 py-3 font-medium">{t("users.invitedBy")}</th>
                <th className="px-5 py-3 text-right font-medium">{t("users.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {data.invites.map((g: RoleGrant) => (
                <tr key={g.id} className="border-b border-slate-50 last:border-0">
                  <td className="px-5 py-3 text-slate-700">{g.email}</td>
                  <td className="px-5 py-3"><RoleBadge role={g.role} /></td>
                  <td className="px-5 py-3 text-slate-500">{g.invitedBy || "—"}</td>
                  <td className="px-5 py-3 text-right">
                    <Button variant="outline" disabled={revoke.isPending} onClick={() => { setError(""); setNotice(""); revoke.mutate(g.id); }}>
                      {t("users.revokeInvite")}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
    </div>
  );
}

function InviteForm({ onDone, onError }: { onDone: (msg: string) => void; onError: (e: Error) => void }) {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Role>("admin");

  const invite = useMutation({
    mutationFn: () => api.inviteUser(email.trim(), role),
    onSuccess: (res) => {
      setEmail("");
      onDone(res.applied ? t("users.promoted", { email: res.user?.email ?? email }) : t("users.invited", { email: res.grant?.email ?? email }));
    },
    onError,
  });

  return (
    <Card className="p-5">
      <h3 className="mb-1 font-semibold">{t("users.inviteTitle")}</h3>
      <p className="mb-4 text-sm text-slate-500">{t("users.inviteHint")}</p>
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => { e.preventDefault(); if (email.trim()) invite.mutate(); }}
      >
        <label className="min-w-56 flex-1 text-sm">
          <span className="mb-1 block text-slate-500">{t("users.email")}</span>
          <Input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="name@rivermont.ca" />
        </label>
        <label className="text-sm">
          <span className="mb-1 block text-slate-500">{t("users.role")}</span>
          <select
            value={role}
            onChange={(e) => setRole(e.target.value as Role)}
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
          >
            <option value="admin">{t("users.role_admin")}</option>
            <option value="staff">{t("users.role_staff")}</option>
          </select>
        </label>
        <Button type="submit" disabled={invite.isPending || !email.trim()}>
          {invite.isPending ? t("users.sending") : t("users.sendInvite")}
        </Button>
      </form>
    </Card>
  );
}

function UserRow({ u, isSelf, busy, onSetRole }: { u: User; isSelf: boolean; busy: boolean; onSetRole: (role: Role) => void }) {
  const { t } = useTranslation();
  return (
    <tr className="border-b border-slate-50 last:border-0">
      <td className="px-5 py-3 text-slate-700">
        {u.name || "—"}
        {isSelf && <span className="ml-2 text-xs text-slate-400">{t("users.you")}</span>}
      </td>
      <td className="px-5 py-3 text-slate-500">{u.email}</td>
      <td className="px-5 py-3"><RoleBadge role={u.role} /></td>
      <td className="px-5 py-3">
        <div className="flex justify-end gap-2">
          {u.role !== "admin" && (
            <Button variant="outline" disabled={busy} onClick={() => onSetRole("admin")}>{t("users.makeAdmin")}</Button>
          )}
          {u.role === "admin" && (
            <Button variant="outline" disabled={busy || isSelf} onClick={() => onSetRole("staff")}>{t("users.makeStaff")}</Button>
          )}
          {/* Revoke all elevated access → resident. Disabled for yourself. */}
          <Button variant="danger" disabled={busy || isSelf} onClick={() => onSetRole("resident")}>{t("users.revoke")}</Button>
        </div>
      </td>
    </tr>
  );
}

function RoleBadge({ role }: { role: Role }) {
  const { t } = useTranslation();
  const tone = role === "admin" ? "brand" : role === "staff" ? "amber" : "slate";
  return <Badge tone={tone}>{t(`users.role_${role}`)}</Badge>;
}
