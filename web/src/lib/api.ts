// Thin REST client over fetch. Requests are same-origin (Vite proxies /api to
// the Go API in dev; Apache does in prod), so the session cookie flows with
// credentials: "include".

const BASE = (import.meta.env.BASE_URL ?? "/").replace(/\/$/, "");

// currentLang reads the language the UI is showing. Sent on every request so
// facility CONTENT matches the surrounding chrome — a French page wrapped around
// English facility text is the exact half-translated result §4.11 rules out.
// Read from localStorage rather than importing i18n to keep this module free of
// that dependency.
function currentLang(): string {
  if (typeof localStorage === "undefined") return "en";
  return localStorage.getItem("lang")?.startsWith("fr") ? "fr" : "en";
}

// withLang appends ?lang= without disturbing an existing query string.
function withLang(path: string): string {
  return path + (path.includes("?") ? "&" : "?") + "lang=" + currentLang();
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}/api${path}`, {
    method,
    credentials: "include",
    headers: body ? { "Content-Type": "application/json", Accept: "application/json" } : { Accept: "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const b = (await res.json().catch(() => ({}))) as { error?: string };
    throw new Error(b.error ?? `Request failed (${res.status})`);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

const get = <T>(p: string) => request<T>("GET", withLang(p));
const post = <T>(p: string, body?: unknown) => request<T>("POST", p, body);
const put = <T>(p: string, body?: unknown) => request<T>("PUT", p, body);
const del = (p: string) => request<void>("DELETE", p);

// --- types -----------------------------------------------------------------

export type Role = "resident" | "staff" | "admin";

export interface User {
  id: string;
  email: string;
  name: string;
  role: Role;
  isResident: boolean;
  address?: string;
  language?: "en" | "fr";   // preferred language for notifications
}

export interface Accessory {
  id: string;
  quantity: number;
  accessory: { id: string; name: string };
}

export interface Facility {
  id: string;
  name: string;
  description: string;
  capacity: number;
  feeCents: number;
  nonResidentFeeCents: number;
  depositCents: number;
  location: string;
  area: string;
  imageUrl: string;
  latitude: number;
  longitude: number;
  requiresApproval: boolean;
  requiresWaiver: boolean;
  minMinutes: number;
  maxMinutes: number;
  bufferMinutes: number;
  stepFreeAccess: boolean;
  accessibleWashroom: boolean;
  beforeInstructions: string;
  afterInstructions: string;
  accessories?: Accessory[];
}

// A facility as served, plus which fields fell back to the default language.
export interface FacilityDetail extends Facility {
  language: "en" | "fr";
  untranslated?: string[];
}

// One language's text for a facility, for the staff editor's tabs.
export interface FacilityTranslation {
  facilityId: string;
  language: "en" | "fr";
  name: string;
  description: string;
  beforeInstructions: string;
  afterInstructions: string;
}

export type SlotStatus = "open" | "booked" | "blackout" | "closed";
export interface CalendarSlot { start: string; status: SlotStatus }
export interface CalendarDay { date: string; label: string; isToday: boolean; slots: CalendarSlot[] }
export interface FacilityCalendar {
  facilityId: string;
  facilityName: string;
  from: string;
  slotMinutes: number;
  openMinute: number;
  closeMinute: number;
  minMinutes: number;
  bufferMinutes: number;
  days: CalendarDay[];
}

export interface Slot {
  start: string;
  end: string;
  available: boolean;
}

export type BookingStatus = "pending" | "confirmed" | "denied" | "cancelled";
// "pending" is a bill raised at a hosted gateway with no money yet — distinct
// from "unpaid" (nothing raised) and from "paid".
export type PaymentStatus = "unpaid" | "pending" | "paid" | "refunded";

export interface Payment {
  bookingId: string;
  amountCents: number;
  status: PaymentStatus;
  // Set only by a gateway that hosts its own checkout (C2's payment broker):
  // where to send the payer. Its presence is what tells the UI not to render a
  // card form.
  payUrl?: string;
}

// RefundObligation is money owed to a resident that this app could not return
// itself, because the gateway takes refund instructions from an operator rather
// than from us.
export interface RefundObligation {
  id: string;
  bookingId: string;
  amountCents: number;
  currency: string;
  reason: string;
  status: "owed" | "settled";
  provider: string;
  providerRef: string;
  settledRef?: string;
  settledCents?: number;
  createdAt: string;
  booking?: Booking;
}

export interface Booking {
  id: string;
  facilityId: string;
  facility?: Facility;
  user?: User;
  startsAt: string;
  endsAt: string;
  status: BookingStatus;
  purpose: string;
  attendance: number;
  feeCents: number;
  payment?: Payment;
  waiver?: WaiverDocument;
}

export interface WaiverDocument {
  bookingId: string;
  contentType: string;
  sizeBytes: number;
}

export interface WaitlistEntry {
  id: string;
  facilityId: string;
  facility?: Facility;
  startsAt: string;
  endsAt: string;
  notifiedAt?: string;
}

export interface RecurringResult {
  recurrenceId: string;
  created: Booking[];
  skipped: string[]; // ISO start times skipped for conflicts
}

export interface AuditEntry {
  index: number;
  timestamp: string;
  message: string;
  action: string;
  actorEmail: string;
  actorId: string;
  targetType: string;
  targetId: string;
}

export interface AuditResponse {
  enabled: boolean;
  entries: AuditEntry[];
}

export type TxnKind = "charge" | "refund";
export type TxnStatus = "succeeded" | "failed";

export interface PaymentTxn {
  id: string;
  createdAt: string;
  bookingId: string;
  paymentId: string;
  kind: TxnKind;
  status: TxnStatus;
  amountCents: number;
  provider: string;
  providerRef: string;
  cardLast4: string;
  message: string;
  facilityName: string;
  userName: string;
  userEmail: string;
}

export interface PaymentSummary {
  collectedCents: number;
  refundedCents: number;
  netCents: number;
  succeeded: number;
  failed: number;
  refunds: number;
}

export interface Reconciliation {
  summary: PaymentSummary;
  transactions: PaymentTxn[];
  total: number;
  page: number;
  pageSize: number;
}

export interface PaymentQuery {
  from?: string;
  to?: string;
  filter?: string;
  page?: number;
  pageSize?: number;
}

export interface RoleGrant {
  id: string;
  email: string;
  role: Role;
  invitedBy: string;
  createdAt: string;
}

export interface AdminUsers {
  users: User[];
  invites: RoleGrant[];
}

export interface InviteResult {
  applied: boolean;
  user?: User;
  grant?: RoleGrant;
}

export type Period = "month" | "quarter" | "year";

export interface FacilityCount { facilityName: string; bookings: number }
export interface SpaceRevenue { facilityName: string; revenueCents: number; utilizationPct: number }
export interface TrendPoint { label: string; utilizationPct: number }

export interface Dashboard {
  period: Period;
  periodLabel: string;
  prevLabel: string;
  revenueCents: number;
  revenueDeltaPct: number;
  bookings: number;
  bookingsDeltaPct: number;
  avgUtilizationPct: number;
  pending: number;
  pendingOver24h: number;
  byFacility: FacilityCount[];
  topSpaces: SpaceRevenue[];
  trend: TrendPoint[];
  residentPct: number;
}

// Editable facility fields for staff create/edit (scalar subset of Facility).
export interface FacilityInput {
  name: string;
  description: string;
  capacity: number;
  feeCents: number;
  nonResidentFeeCents: number;
  depositCents: number;
  location: string;
  area: string;
  imageUrl: string;
  latitude: number;
  longitude: number;
  requiresApproval: boolean;
  requiresWaiver: boolean;
  minMinutes: number;
  maxMinutes: number;
  bufferMinutes: number;
  stepFreeAccess: boolean;
  accessibleWashroom: boolean;
  beforeInstructions: string;
  afterInstructions: string;
}

export interface Blackout {
  id: string;
  facilityId: string;
  startsAt: string;
  endsAt: string;
  reason: string;
}

// --- entitlements (§P2-5.11a) ----------------------------------------------

export type EntitlementType = "residency" | "subsidy";

export interface EntitlementField {
  key: string;
  label: string;
  placeholder?: string;
  required: boolean;
}

// The provider's published input contract — what it needs *from the applicant*.
// It deliberately does not describe what makes the check pass.
export interface EntitlementDescriptor {
  type: EntitlementType;
  provider: string;
  version: string;
  statement: string;
  fields: EntitlementField[];
  evidence?: string[];
}

export interface Determination {
  type: EntitlementType;
  category: string;
  provider: string;
  evaluatedAt: string;
  validUntil?: string;
  stale: boolean; // served from cache because the provider was unreachable
}

export interface EntitlementNotice {
  type: EntitlementType;
  reason: "not_held" | "needs_proving" | "unavailable";
}

export interface EntitlementSet {
  live: Determination[];
  notices: EntitlementNotice[];
}

export interface ProveResult {
  outcome: "granted" | "denied";
  entitlements: EntitlementSet;
}

// --- cancellation policy (§4.7, §4.9) --------------------------------------

export interface RefundTier {
  hoursBefore: number;
  refundPercent: number;
}

export interface CancellationPolicy {
  id: string;
  facilityId?: string;      // absent = the municipality-wide default
  name: string;
  nonRefundableCents: number;
  modificationCutoffHours: number;
  tiers?: RefundTier[];
}

// What cancelling right now would return. Computed by the same code the cancel
// path uses, so the figure shown is the figure issued.
export interface RefundQuote {
  policyName: string;
  paidCents: number;
  refundCents: number;
  refundPercent: number;
  hoursUntilStart: number;
  appliedTierHours: number;
  explanation: string;
}

// --- payment modules (§4.7) ------------------------------------------------

export type PaymentKind = "mock" | "stripe" | "moneris";

export interface PaymentModule {
  kind: PaymentKind;
  name: string;
  summary: string;
  available: boolean;      // false = selectable as a recorded decision, not yet functional
  supportsRefund: boolean;
  supportsHold: boolean;   // separate auth/capture — what deposits need
  secretEnv?: string;      // API key comes from this env var, never the form
  fields: CalendarField[]; // same {key,label,placeholder,required} shape
}

export interface PaymentSettings {
  selected: PaymentKind;
  effective: PaymentKind;  // what the app actually charges through
  config: Record<string, string>;
  connected: boolean;
  updatedById?: string;
  fallbackNotes?: string;
}

export interface PaymentSettingsResponse {
  modules: PaymentModule[];
  settings: PaymentSettings;
}

// --- calendar integration (§4.6) -------------------------------------------

export type CalendarKind = "none" | "ics" | "google" | "microsoft";

export interface CalendarField {
  key: string;
  label: string;
  placeholder?: string;
  required: boolean;
}

export interface CalendarModule {
  kind: CalendarKind;
  name: string;
  summary: string;
  twoWay: boolean;
  available: boolean;   // false = selectable as a recorded decision, not yet functional
  secretEnv?: string;   // credentials come from this env var, never from the form
  fields: CalendarField[];
}

export interface CalendarSettings {
  selected: CalendarKind;   // what the municipality chose
  effective: CalendarKind;  // what the app is actually running
  config: Record<string, string>;
  connected: boolean;
  updatedById?: string;
  fallbackNotes?: string;
}

export interface CalendarSettingsResponse {
  modules: CalendarModule[];
  settings: CalendarSettings;
}

export interface FacilityFilter {
  minCapacity?: number;
  free?: boolean;
  accessories?: string[];   // all must be present, not any (§4.3)
  area?: string;
  stepFree?: boolean;
  accessibleWashroom?: boolean;
  maxFeeCents?: number;
  date?: string;  // YYYY-MM-DD — with start+end, restricts to facilities free then
  start?: string; // HH:MM
  end?: string;   // HH:MM
}

// FacilityFilterOptions is the vocabulary the filter panel offers: the areas
// facilities are actually placed in and the accessories they actually have.
export interface FacilityFilterOptions {
  areas: string[];
  accessories: string[];
}

// Note there is no `resident` field. The cost filter prices against what the
// signed-in viewer would actually pay, and the server resolves that from the
// session — a client-supplied flag would hand anyone the resident rate.

// --- api --------------------------------------------------------------------

export const api = {
  me: () => get<User | null>("/auth/me"),
  // Persist the language choice so notifications — sent from the server, days
  // later, often to a different device — arrive in the language they picked.
  setLanguage: (language: "en" | "fr") => put<{ language: string }>("/me/language", { language }),
  // Entitlements: submit evidence, never an outcome — the provider decides.
  entitlements: () => get<EntitlementSet>("/entitlements"),
  entitlementDescriptor: (type: EntitlementType) =>
    get<EntitlementDescriptor>(`/entitlements/${type}/descriptor`),
  proveEntitlement: (type: EntitlementType, inputs: Record<string, string>) =>
    post<ProveResult>(`/entitlements/${type}/prove`, { inputs }),
  loginUrl: (returnTo?: string) => {
    if (!returnTo) return `${BASE}/api/auth/login`;
    const q = new URLSearchParams({ return_to: returnTo });
    return `${BASE}/api/auth/login?${q.toString()}`;
  },
  // Returns C2's RP-initiated logout URL (when OIDC is configured); the caller
  // must navigate to it so the C2 session ends too, not just the local one.
  logout: () => post<{ status: string; logoutUrl?: string }>("/auth/logout"),
  homeUrl: () => `${BASE}/`,

  listFacilities: (f: FacilityFilter = {}) => {
    const q = new URLSearchParams();
    if (f.minCapacity) q.set("minCapacity", String(f.minCapacity));
    if (f.free) q.set("free", "true");
    // Repeated rather than comma-joined, so an accessory whose name contains a
    // comma still round-trips.
    for (const a of f.accessories ?? []) q.append("accessory", a);
    if (f.area) q.set("area", f.area);
    if (f.stepFree) q.set("stepFree", "true");
    if (f.accessibleWashroom) q.set("accessibleWashroom", "true");
    if (f.maxFeeCents) q.set("maxFee", String(f.maxFeeCents));
    if (f.date && f.start && f.end) {
      q.set("date", f.date);
      q.set("start", f.start);
      q.set("end", f.end);
    }
    const qs = q.toString();
    return get<Facility[]>(`/facilities${qs ? `?${qs}` : ""}`);
  },
  facilityFilterOptions: () => get<FacilityFilterOptions>("/facilities/filter-options"),
  getFacility: (id: string) => get<FacilityDetail>(`/facilities/${id}`),
  facilityTranslations: (id: string) => get<FacilityTranslation[]>(`/staff/facilities/${id}/translations`),
  saveFacilityTranslation: (id: string, t: Omit<FacilityTranslation, "facilityId">) =>
    put<FacilityTranslation[]>(`/staff/facilities/${id}/translations`, t),
  availability: (id: string, date: string) => get<Slot[]>(`/facilities/${id}/availability?date=${date}`),
  facilityCalendar: (id: string, from: string, days?: number) =>
    get<FacilityCalendar>(`/facilities/${id}/calendar?from=${from}${days ? `&days=${days}` : ""}`),

  createBooking: (b: { facilityId: string; start: string; end: string; purpose: string; attendance: number }) =>
    post<Booking>("/bookings", b),
  createRecurring: (b: { facilityId: string; start: string; end: string; purpose: string; attendance: number; repeatWeeks: number }) =>
    post<RecurringResult>("/bookings", b),
  myBookings: () => get<Booking[]>("/bookings/mine"),
  // The cancellation returns the booking plus what it refunded.
  cancelBooking: (id: string) => post<Booking & { refund?: RefundQuote }>(`/bookings/${id}/cancel`),
  refundQuote: (id: string) => get<RefundQuote>(`/bookings/${id}/refund-quote`),
  cancellationPolicy: (facilityId: string) =>
    get<CancellationPolicy>(`/facilities/${facilityId}/cancellation-policy`),
  reschedule: (id: string, w: { start: string; end: string }) => post<Booking>(`/bookings/${id}/reschedule`, w),
  pay: (id: string, card: string) => post<Payment>(`/bookings/${id}/pay`, { card }),
  inviteUrl: (id: string) => `${BASE}/api/bookings/${id}/invite.ics`,
  waiverUrl: (id: string) => `${BASE}/api/bookings/${id}/waiver`,
  waiverTemplateUrl: () => `${BASE}/api/waiver-template.pdf`,
  uploadWaiver: async (id: string, file: File) => {
    const fd = new FormData();
    fd.append("file", file);
    const res = await fetch(`${BASE}/api/bookings/${id}/waiver`, { method: "POST", credentials: "include", body: fd });
    if (!res.ok) { const b = (await res.json().catch(() => ({}))) as { error?: string }; throw new Error(b.error ?? `Upload failed (${res.status})`); }
    return res.json() as Promise<Booking>;
  },

  joinWaitlist: (facilityId: string, w: { start: string; end: string }) =>
    post<WaitlistEntry>(`/facilities/${facilityId}/waitlist`, w),
  myWaitlist: () => get<WaitlistEntry[]>("/waitlist/mine"),
  leaveWaitlist: (id: string) => del(`/waitlist/${id}`),

  pendingBookings: () => get<Booking[]>("/staff/bookings/pending"),
  approve: (id: string) => post<Booking>(`/staff/bookings/${id}/approve`),
  deny: (id: string) => post<Booking>(`/staff/bookings/${id}/deny`),
  refund: (id: string) => post<Payment>(`/staff/bookings/${id}/refund`),
  report: (period: Period = "quarter") => get<Dashboard>(`/staff/reports/summary?period=${period}`),
  // Admin-only user/role management.
  adminUsers: () => get<AdminUsers>("/staff/users"),
  inviteUser: (email: string, role: Role) => post<InviteResult>("/staff/users/invite", { email, role }),
  setUserRole: (id: string, role: Role) => put<User>(`/staff/users/${id}/role`, { role }),
  revokeInvite: (id: string) => del(`/staff/users/invites/${id}`),

  // Calendar integration: staff can read which module the city runs on; only an
  // admin can change it.
  calendarSettings: () => get<CalendarSettingsResponse>("/staff/calendar-settings"),
  setCalendarSettings: (kind: CalendarKind, config: Record<string, string>) =>
    put<CalendarSettingsResponse>("/staff/calendar-settings", { kind, config }),

  refundObligations: (status: "owed" | "settled" = "owed") =>
    get<RefundObligation[]>(`/staff/refund-obligations?status=${status}`),
  paymentSettings: () => get<PaymentSettingsResponse>("/staff/payment-settings"),
  setPaymentSettings: (kind: PaymentKind, config: Record<string, string>) =>
    put<PaymentSettingsResponse>("/staff/payment-settings", { kind, config }),

  payments: (p: PaymentQuery = {}) => {
    const q = new URLSearchParams();
    if (p.from) q.set("from", p.from);
    if (p.to) q.set("to", p.to);
    if (p.filter && p.filter !== "all") q.set("filter", p.filter);
    if (p.page) q.set("page", String(p.page));
    if (p.pageSize) q.set("pageSize", String(p.pageSize));
    const qs = q.toString();
    return get<Reconciliation>(`/staff/payments${qs ? `?${qs}` : ""}`);
  },
  auditLog: () => get<AuditResponse>("/staff/audit"),

  // Staff facility management (§4.8)
  createFacility: (f: FacilityInput) => post<Facility>("/staff/facilities", f),
  updateFacility: (id: string, f: FacilityInput) => put<Facility>(`/staff/facilities/${id}`, f),
  deleteFacility: (id: string) => del(`/staff/facilities/${id}`),
  listBlackouts: (id: string) => get<Blackout[]>(`/staff/facilities/${id}/blackouts`),
  addBlackout: (id: string, b: { start: string; end: string; reason: string }) =>
    post<Blackout>(`/staff/facilities/${id}/blackouts`, b),
  removeBlackout: (id: string, blackoutId: string) => del(`/staff/facilities/${id}/blackouts/${blackoutId}`),
};
