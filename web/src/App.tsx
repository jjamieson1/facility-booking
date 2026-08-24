import { useEffect, useRef, useState } from "react";
import {
  Link,
  NavLink,
  Outlet,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "./lib/auth";
import { brand, documentTitle } from "./lib/brand";
import { api } from "./lib/api";
import { setLanguage } from "./lib/i18n";
import { Button } from "./components/ui";
import { FacilityList } from "./pages/FacilityList";
import { FacilityDetail } from "./pages/FacilityDetail";
import { FacilityCalendar } from "./pages/FacilityCalendar";
import { MapView } from "./pages/MapView";
import { BookingDetail } from "./pages/BookingDetail";
import { MyBookings } from "./pages/MyBookings";
import { StaffQueue } from "./pages/StaffQueue";
import { StaffReports } from "./pages/StaffReports";
import { StaffPayments } from "./pages/StaffPayments";
import { StaffUsers } from "./pages/StaffUsers";
import { StaffFacilities } from "./pages/StaffFacilities";
import { StaffAudit } from "./pages/StaffAudit";
import { StaffCalendar } from "./pages/StaffCalendar";
import { StaffPaymentSettings } from "./pages/StaffPaymentSettings";
import { Spinner } from "./components/ui";

// The tab title comes from the brand config, so index.html's copy is only the
// pre-hydration fallback and never a second place to edit.
function useBrandTitle() {
  useEffect(() => {
    document.title = documentTitle();
  }, []);
}

export default function App() {
  const { user, isStaff, login, logout, loading } = useAuth();
  const { t, i18n } = useTranslation();
  useBrandTitle();

  return (
    <div className="min-h-screen">
      <a
        href="#main"
        className="sr-only rounded-lg bg-brand-500 px-4 py-2 text-white focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50"
      >
        {t("a11y.skipToContent")}
      </a>
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-4 px-6 py-3">
          <Link to="/" className="flex items-center gap-2">
            <span
              aria-hidden="true"
              className="grid h-8 w-8 place-items-center rounded-lg bg-brand-500 text-sm font-bold text-white"
            >
              {brand.mark}
            </span>
            <span className="text-lg font-semibold">{brand.name}</span>
          </Link>

          <nav
            aria-label={t("a11y.mainNav")}
            className="flex items-center gap-1 text-sm"
          >
            <TopLink to="/">{t("nav.facilities")}</TopLink>
            <TopLink to="/availability">{t("nav.availability")}</TopLink>
            <TopLink to="/map">{t("nav.map")}</TopLink>
            {user && <TopLink to="/my-bookings">{t("nav.myBookings")}</TopLink>}
            {isStaff && <AdminMenu />}
          </nav>

          <div className="flex items-center gap-3">
            <LangToggle current={i18n.language} user={user} />
            {loading ? null : user ? (
              <>
                <span className="hidden text-sm text-slate-600 sm:inline">
                  {(user.name || user.email) && (
                    <span className="font-medium text-slate-800">
                      {user.name || user.email}
                    </span>
                  )}
                  <span className="text-slate-500">
                    {user.name || user.email ? " · " : ""}
                    {user.role}
                  </span>
                </span>
                <Button variant="outline" onClick={() => void logout()}>
                  {t("auth.signOut")}
                </Button>
              </>
            ) : (
              <Button onClick={login}>{t("auth.signIn")}</Button>
            )}
          </div>
        </div>
      </header>

      <main id="main" className="mx-auto max-w-6xl px-6 py-8">
        <Routes>
          <Route path="/" element={<FacilityList />} />
          <Route path="/availability" element={<FacilityCalendar />} />
          <Route path="/map" element={<MapView />} />
          <Route path="/facilities/:id" element={<FacilityDetail />} />
          <Route element={<RequireAuth />}>
            <Route path="/bookings/:id" element={<BookingDetail />} />
            <Route path="/my-bookings" element={<MyBookings />} />
            <Route path="/staff" element={<StaffQueue />} />
            <Route path="/staff/facilities" element={<StaffFacilities />} />
            <Route path="/staff/reports" element={<StaffReports />} />
            <Route path="/staff/payments" element={<StaffPayments />} />
            <Route path="/staff/users" element={<StaffUsers />} />
            <Route path="/staff/calendar" element={<StaffCalendar />} />
            <Route path="/staff/payment-settings" element={<StaffPaymentSettings />} />
            <Route path="/staff/audit" element={<StaffAudit />} />
          </Route>
        </Routes>
      </main>
    </div>
  );
}

// RequireAuth redirects anonymous users to OIDC login, preserving the current
// path so they land back on the intended page after callback.
function RequireAuth() {
  const { user, loading, loginTo } = useAuth();
  const loc = useLocation();

  useEffect(() => {
    if (loading || user) return;
    loginTo(`${loc.pathname}${loc.search}`);
  }, [loading, user, loginTo, loc.pathname, loc.search]);

  if (loading || !user) return <Spinner />;
  return <Outlet />;
}

// AdminMenu groups the staff-only links into one dropdown to keep the top nav
// uncluttered. Accessible: aria-haspopup/expanded, closes on Escape or outside click.
function AdminMenu() {
  const { t } = useTranslation();
  const { isAdmin } = useAuth();
  const { pathname } = useLocation();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const active = pathname.startsWith("/staff");

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node))
        setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const items = [
    { to: "/staff", label: t("nav.approvals"), end: true },
    { to: "/staff/facilities", label: t("nav.manage"), end: false },
    { to: "/staff/reports", label: t("nav.reports"), end: false },
    { to: "/staff/payments", label: t("nav.payments"), end: false },
    { to: "/staff/calendar", label: t("nav.calendarSettings"), end: false },
    ...(isAdmin ? [{ to: "/staff/payment-settings", label: t("nav.paymentSettings"), end: false }] : []),
    ...(isAdmin
      ? [{ to: "/staff/users", label: t("nav.users"), end: false }]
      : []),
    { to: "/staff/audit", label: t("nav.audit"), end: false },
  ];

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        className={`flex items-center gap-1 rounded-lg px-3 py-1.5 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 ${active ? "bg-brand-50 text-brand-700" : "text-slate-600 hover:bg-slate-100"}`}
      >
        {t("nav.admin")}
        <svg
          aria-hidden="true"
          width="12"
          height="12"
          viewBox="0 0 12 12"
          className={`transition-transform ${open ? "rotate-180" : ""}`}
        >
          <path
            d="M2 4l4 4 4-4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </button>
      {open && (
        <div
          role="menu"
          className="absolute left-0 z-20 mt-1 w-44 overflow-hidden rounded-lg border border-slate-200 bg-white py-1 shadow-lg"
        >
          {items.map((it) => (
            <NavLink
              key={it.to}
              to={it.to}
              end={it.end}
              role="menuitem"
              onClick={() => setOpen(false)}
              className={({ isActive }) =>
                `block px-4 py-2 text-sm ${isActive ? "bg-brand-50 font-medium text-brand-700" : "text-slate-700 hover:bg-slate-50"}`
              }
            >
              {it.label}
            </NavLink>
          ))}
        </div>
      )}
    </div>
  );
}

// LangToggle switches between English and French (§4.11).
function LangToggle({ current, user }: { current: string; user: unknown }) {
  const { t } = useTranslation();
  const lang = current?.startsWith("fr") ? "fr" : "en";
  const names: Record<string, string> = { en: "English", fr: "Français" };
  return (
    <div
      role="group"
      aria-label={t("a11y.language")}
      className="flex overflow-hidden rounded-lg border border-slate-300 text-xs font-medium"
    >
      {(["en", "fr"] as const).map((l) => (
        <button
          key={l}
          type="button"
          onClick={() => {
            setLanguage(l);
            // Anonymous visitors have nothing to store it against; the UI still
            // switches, and it persists once they sign in and toggle again.
            if (user) void api.setLanguage(l).catch(() => {});
          }}
          aria-pressed={lang === l}
          aria-label={names[l]}
          className={`px-2 py-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-500 ${lang === l ? "bg-brand-500 text-white" : "bg-white text-slate-600 hover:bg-slate-50"}`}
        >
          {l.toUpperCase()}
        </button>
      ))}
    </div>
  );
}

function TopLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <NavLink
      to={to}
      end={to === "/"}
      className={({ isActive }) =>
        `rounded-lg px-3 py-1.5 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 ${isActive ? "bg-brand-50 text-brand-700" : "text-slate-600 hover:bg-slate-100"}`
      }
    >
      {children}
    </NavLink>
  );
}
