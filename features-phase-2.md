# Features — Phase 2

**Gap analysis of `requirements-vers-2.md` (PRD v2.0, 11 Aug 2026) against the code as built.**

Written 13 Aug 2026. v2 widens the product from a facility booking app into a full recreation
platform with five new or substantially expanded modules. What we shipped (Phases 1–6 + §4.9–4.11
extras) is essentially **one** of those modules — "Facility and asset booking" — plus a partial
"Financial operations" and "Administration".

v2 §4 also specified multi-tenancy. **That is descoped** (see §P2-4); this remains a
single-municipality deployment.

Decisions recorded in `Open-questions.md` are folded in throughout and summarised in §P2-8 — several
of them add scope this gap analysis would not otherwise carry.

Every "what we have" claim below was verified against the code, not against `CLAUDE.md`.

---

## 0. How to read this

| Verdict     | Meaning                                                                     |
| :---------- | :-------------------------------------------------------------------------- |
| **DONE**    | v2 requirement is already met; no phase-2 work.                             |
| **PARTIAL** | Something exists but falls short of v2's acceptance criteria. Scope listed. |
| **MISSING** | Nothing in the codebase addresses it.                                       |
| **PROCESS** | Not a code deliverable — a commercial, hosting or documentation commitment. |
| **DESCOPED** | Was in v2, removed from scope by agreement.                                |
| **DESIGN AGREED** | Design settled in review; scope and constraints recorded, not yet built. |

Section numbers match `requirements-vers-2.md` so the two documents can be read side by side.

---

## 1. Summary

| v2 § | Requirement                             | Verdict      | Phase-2 work                                                                                                   |
| :--- | :-------------------------------------- | :----------- | :------------------------------------------------------------------------------------------------------------- |
| 4    | Platform and tenancy                    | **DESCOPED** | Multi-tenancy removed from scope by agreement (Aug 2026); stand-alone items rehomed                            |
| 5.1  | Asset directory — hierarchy, sub-spaces | **PARTIAL**  | Site/building/space/sub-space model; station division; **hierarchy-aware conflict detection**; pagination      |
| 5.2  | Availability — rollover, mass update    | **PARTIAL**  | Seasonal rollover with preview; mass update with reverse; rules engine per asset type                          |
| 5.3  | Search by parameters                    | **PARTIAL**  | Cost-range, location/area, accessibility filters; guided empty state                                           |
| 5.4  | Search by date and time                 | **DONE**     | —                                                                                                              |
| 5.5  | Booking and approval                    | **PARTIAL**  | Priority/allocation windows; cross-hierarchy concurrency; series conflict preview; approval conditions; configurable approval routing |
| 5.5a | Guest booking                            | **DESIGN AGREED** | Not a v2 section — guest sessions, magic-link reclaim, and the `RequireAuth` split that must precede them |
| 5.6  | Calendar sync                           | **PARTIAL**  | Admin-selectable calendar provider (M365 / Google / other) behind an interface                                 |
| 5.7  | Programs and registration               | **MISSING**  | Entire module                                                                                                  |
| 5.8  | Memberships                             | **MISSING**  | Entire module                                                                                                  |
| 5.9  | Admissions, retail and POS              | **MISSING**  | Entire module                                                                                                  |
| 5.10 | Events, permits and workflow            | **MISSING**  | Entire module (only waiver upload exists)                                                                      |
| 5.11 | Financial operations                    | **PARTIAL**  | Fee engine, rate blending, tax, invoicing, GL coding + export, reconciliation, report builder, admin-selectable provider |
| 5.11a | Entitlement determination                | **DESIGN AGREED** | Not a v2 section — provider-evaluated, discoverable, expiring entitlements (residency + §5.8 subsidy) replacing the `IsResident` boolean; **prerequisite for the fee engine** |
| 5.12 | Staff back-office                       | **PARTIAL**  | Department-scoped permissions, bulk import/edit, staff dashboard, training environment, module enablement      |
| 5.13 | My account                              | **PARTIAL**  | Unify registrations, memberships, permits, counter history                                                     |
| 5.14 | Notifications                           | **PARTIAL**  | Real email delivery, SMS, configurable templates                                                               |
| 5.15 | Further capabilities                    | **PARTIAL**  | Website embedding; configurable terminology; city branding; **bilingual facility content**; demand reporting (map, waitlist, public view, bilingual UI all DONE) |
| 6    | Data model                              | **PARTIAL**  | 7 new entities + 5 expanded (detailed in §P2-6)                                                                |
| 7.1  | Accessibility                           | **PARTIAL**  | Contrast audit, mobile pass, tablet-usable staff UI, ACR document                                              |
| 7.2  | Security and privacy                    | **PARTIAL**  | Configurable SSO + MFA, PCI-DSS, residency, MFIPPA/FOIP, config audit, DR/RPO/RTO                              |
| 7.3  | Performance and scale                   | **MISSING**  | Pagination, indexing, caching, horizontal scale, load-test evidence                                            |
| 7.4  | Integration and portability             | **MISSING**  | Versioned public API, migration tooling, data export, GL + calendar integrations, pluggable integration interface |
| 7.5  | Service operations                      | **PROCESS**  | SLA, support model, maintenance windows, roadmap                                                               |

**Five modules' worth of new build.** Realistically this is not one phase; §P2-9 proposes a split.

---

## P2-4. Platform and tenancy — DESCOPED

v2 §4 specifies a multi-tenant platform, and v2 §2.1 adds the goal of serving many municipalities
from one codebase. **Both were removed from scope by agreement with the customer (Aug 2026).**
This remains a single-municipality deployment, so there is no tenant entity, no per-request tenant
resolution, and no per-tenant branding to build. **Staging → production configuration promotion is
also confirmed not required** — configuration is made in the environment where it applies. Note this
is a decision, not a consequence: configuration *versioning and rollback* survive in P2-7.2, and the
*training environment* survives in P2-5.12; neither depends on promotion.

v2 §2.1's other new goal — adopting the platform one module at a time, without disrupting live
modules — does **not** depend on tenancy and survives as module enablement (P2-5.12).

The remaining §4 items that stand alone for a single municipality are carried into their natural
homes rather than dropped:

| §4 item                                  | Now covered by |
| :--------------------------------------- | :------------- |
| Modular activation                       | P2-5.12        |
| Training environment                     | P2-5.12        |
| Configurable notification templates      | P2-5.14        |
| Configurable terminology                 | P2-5.15        |
| Configurable SSO against the city's IdP  | P2-7.2         |
| Versioned, attributable, reversible config | P2-7.2       |

---

## P2-5.1. Asset directory — PARTIAL

**What we have.** `domain.Facility` with name, description, capacity, fee, deposit, location,
image, lat/long, before/after instructions, `StepFreeAccess`, `AccessibleWashroom`, and
`FacilityAccessory` join rows carrying quantities. A self-referential `ParentID *string` exists.
Directory + detail pages in `web/src/pages/FacilityList.tsx` / `FacilityDetail.tsx`.

**Missing.**

1. **Hierarchy semantics.** `ParentID` is stored but carries no meaning: nothing distinguishes
   site / building / space / sub-space, and there is no flag for "parent is bookable in its own
   right" vs "bookable only through its children". Needs an `AssetKind` (or level) plus
   `BookableDirectly bool`.

2. **Station division** (pool → lanes, gym → courts, hall → halves). Nothing models this.

3. **Hierarchy-aware conflict detection — a correctness gap, not just a missing feature.**
   `booking.loadWindow` (`internal/booking/service.go:98`) scopes its blackout and overlap
   queries to a single `facility_id`:

   ```go
   Where("facility_id = ? AND status IN ? AND ends_at > ? AND starts_at < ?", facilityID, …)
   ```

   So today, booking court 2 does **not** block the whole-gymnasium asset, and booking the
   gymnasium does not block its courts. v2 §5.5 requires the block to hold "across parent and
   child assets", and v2 §5.1's acceptance criterion ("court 2 booked → courts 1 and 3 available,
   whole-gymnasium unavailable") fails as built. Fix: resolve the ancestor+descendant closure of
   the target asset inside the transaction and scan bookings across that whole set, keeping the
   `SELECT … FOR UPDATE` lock. Availability rendering (`internal/availability`,
   `facility.Calendar`) needs the same closure or the calendar will show open slots the booking
   endpoint then rejects.

4. **Floor plans** — only a single `ImageURL`; v2 wants photos (plural) and floor plans.

5. **Fuller accessibility details** — v2 names accessible parking and hearing loop; we have two
   booleans.

6. **SCALE: 8,000+ assets across 20+ facilities.** `facility.Service.List`
   (`internal/facility/service.go:33`) returns _every_ matching row — no `LIMIT`, no cursor, no
   count. It preloads `Accessories.Accessory` for all of them, and `FacilityList.tsx` renders the
   lot. At 8,000 assets this is a slow query and a multi-megabyte JSON payload. Needs pagination
   (API + UI), server-side sort, and indexes for the filter columns.

**Also DONE:** free assets already display "Free" explicitly (`FeeCents == 0`), satisfying that
acceptance criterion.

---

## P2-5.2. Availability and scheduling — PARTIAL

**What we have.** `domain.AvailabilityRule` (per-weekday open/close minutes), `domain.Blackout`
(date-range closures with a reason), min/max duration and buffer minutes on the facility, per-day
slot generation in `internal/availability`, and a public weekly/monthly calendar
(`GET /api/facilities/{id}/calendar` → `web/src/pages/FacilityCalendar.tsx`) that distinguishes
open / booked / blackout. Staff manage blackouts via `/api/staff/facilities/{id}/blackouts`.

**Missing.**

- **No season concept at all.** Rules are open-ended weekday patterns with no effective-date
  range, so "roll a season forward" has nothing to roll. Needs a `Season` (or effective-dated
  rule sets) before rollover can exist.
- **Seasonal rollover** across ~500 assets in one operation, carrying schedules, rules _and_
  rates forward, with a **preview of what will change before commit**.
- **Mass update tooling** across a selected asset set, **reversible** — which implies a recorded
  change-set with a stored before-state, not just a bulk `UPDATE`.
- **A configurable booking-rules engine** (from `Open-questions.md`). Min duration, max duration
  and buffer already exist as per-facility columns on `domain.Facility` and are enforced in
  `internal/availability`. Two things are missing: **cancellation windows** (currently hardcoded —
  see P2-5.13) and the ability to set rules **per asset type** as well as per individual asset,
  which needs an asset-type dimension to hang defaults on rather than editing assets one by one.

---

## P2-5.3. Search by parameters — PARTIAL

**What we have.** `facility.Filter` (`internal/facility/service.go:26`) supports exactly three
constraints: `MinCapacity`, `FreeOnly`, and a single `Accessory` name — exposed as
`?minCapacity=&free=&accessory=`.

**Missing.**

- **Cost range.** Only a free-only boolean; v2 asks for a range.
- **Location or area.** `Location` is free text with no area/zone dimension to filter on.
- **Accessibility needs** as a filter (the fields exist on the model but aren't filterable).
- **Multiple required amenities** — the filter takes one accessory name, not a set.
- **Guided empty state** suggesting which filter to relax. Today an empty result renders as an
  empty list.

Small, cheap, and directly resident-facing — good early phase-2 work.

---

## P2-5.4. Search by date and time — DONE

`?date=&start=&end=` ANDs with the parameter filters and returns only assets free for the whole
window (`internal/httpapi/facilities.go:29`, `facility.Service.Search`). Meets v2 §5.4.

---

## P2-5.5. Booking request and approval — PARTIAL

**What we have.** Booking captures asset, window, purpose, attendance and residency-at-booking.
Auto-confirm vs `RequiresApproval` per facility. Pending → confirmed / denied / cancelled with
notification at each step, staff approve/deny/refund, cancel, reschedule (§4.9). Double-booking
is prevented inside a `Transaction` with `clause.Locking{Strength: "UPDATE"}` on the overlap scan
(`internal/booking/service.go:47,110`) — genuine row-level locking, with a concurrency test in
`internal/booking/service_test.go`. Weekly recurring bookings grouped by `RecurrenceID`, skipping
conflicting weeks.

**Missing.**

- **Concurrency across the hierarchy** — the lock is per-`facility_id` only. See P2-5.1 item 3.
  The v2 concurrency criterion passes for a single asset and fails for parent/child.
- **Allocation priority windows.** No concept of an affiliated/resident-only booking period
  before general release, and no "general booking opens on <date>" message. Needs a per-asset (or
  per-season) window with an eligibility rule, enforced server-side.
- **Recurring/seasonal as a single application with conflicts reported before submission.** Ours
  silently _skips_ conflicting weeks at submission time (`RequestRecurring`,
  `internal/booking/service.go:182`); v2 wants the conflicts surfaced for the applicant to
  resolve first, and the series treated as one application.
- **Approval carrying conditions, fees or required documents.** Approval is a plain status
  transition. `RequiresWaiver` is the only document gate, and it is a facility-level flag set
  before booking, not a condition a staff member attaches at approval time.
- Attendance is captured but not validated against capacity.
- **Guest booking with email verification** (from `Open-questions.md`, not in v2). Design settled in
  review — see §P2-5.5a.
- **Configurable approval structure** (from `Open-questions.md`). Approval is currently a flat
  staff/admin role check — any staff user may approve anything. The decision taken is
  admin-configurable routing: a single approver, per-facility approvers, or per-department
  approvers (shares the department dimension with P2-5.12), with an optional push of the approval
  to a CRM system (a new integration surface — see P2-7.4).

---

## P2-5.5a. Guest booking — DESIGN AGREED

Not a v2 section. `Open-questions.md` commits to "a guest booking option with email verification";
this is the design settled in review (Aug 2026).

### Why it is not a small change

Every booking path sits behind `auth.RequireAuth` (`internal/httpapi/router.go:91`), and
`Booking.UserID` is non-optional, so booking without an account is currently impossible. Ownership
checks are spread across `bk.get`, `bk.cancel`, `bk.reschedule`, `bk.pay`, waiver upload/download
and `bookings/mine`.

### The decision

**"Continue as guest" creates a real session backed by a real user row.** Guests get
`Role = RoleGuest` and a synthetic `Subject` of `guest:<uuid>`.

The reason to prefer this over a nullable `Booking.UserID` plus a `GuestEmail` column: the nullable
variant forks every ownership check into "owned by user OR matches guest email", which is precisely
the shape that produces IDOR bugs. A real session keeps one authorization model and leaves
`Booking`, `Payment`, `WaiverDocument` and `WaitlistEntry` foreign keys untouched.

**The verified email is the durable identity and the reclaim key — not the guest identifier.** A
guest id that can reclaim bookings is password-equivalent: it reads and cancels bookings and
downloads waivers, which are insurance documents and signed PII. Put it in a URL and it leaks
through browser history, shared links and server logs. (`Referrer-Policy` is
`strict-origin-when-cross-origin`, so cross-origin Referer leakage is already covered — the other
paths are not.) Reclaiming from a new device therefore sends a **short-lived, single-use,
hashed-at-rest magic link** to the verified address. The guest identifier stays an internal account
key and a cookie-scoped handle for the current browser, never a credential the user carries.

### Correctness and security constraints

1. **`RequireAuth` must split into two tiers before guest sessions exist.**
   `auth/middleware.go:36` is literally `if FromContext(r.Context()) == nil` — "any session". The
   moment guests hold sessions, every route behind it opens to them. Each existing `RequireAuth`
   route must be explicitly assigned to "any session including guest" or "real account only". This
   audit **is** the work item; the login button is the easy part.
2. **`Subject` is a `uniqueIndex`.** Guests need the synthetic `guest:<uuid>` — a second guest with
   `""` violates the constraint.
3. **Logout must branch.** It builds C2's `end_session` URL from `Session.IDToken`
   (`domain.Session`); a guest has no ID token, so that path must clear the local session and return
   no `logoutUrl`. Getting this wrong reintroduces the "logout does nothing" class of bug.
4. **`RoleRank` defaults unknown roles to resident** (`internal/domain/user.go`). Adding `RoleGuest`
   turns that default into a privilege-escalation footgun, so `RoleRank`, `ValidRole` and the
   `default:resident` GORM tag must change together.
5. **Verify the email before the booking holds a slot.** `Booking.Active()` counts `StatusPending`,
   so pending bookings block their slot for everyone. Guest booking removes the friction that
   currently prevents junk requests, so an unverified guest could block a facility's calendar with
   unpaid holds. Expire unverified pending bookings aggressively, and rate-limit by email and IP —
   note there is **no rate limiting anywhere in the router today**.
6. **Account merge is a real migration.** When a guest later signs in via C2 with the same verified
   email, move bookings, payments, waivers and waitlist entries to the C2-keyed user and retire the
   guest row — transactionally and idempotently. `User.Email` is `index`, not `uniqueIndex`, so
   duplicate emails across a guest row and a C2 row are permitted; email is therefore not a safe
   sole lookup key.
7. **Guest pricing goes through entitlement determination** (P2-5.11a), never self-assertion. A guest has
   no enrolment reference, so they get the proving form or normal rates.
8. **Guest records are personal data with no account to manage them.** §7.2's MFIPPA/FOIP access and
   correction support has to cover them, and they need a retention policy.

### Still open

- **Guest session lifetime.** Longer than a C2 session is friendlier for someone returning days
  later, but a longer-lived bearer session is more exposure. Not decided.
- **Header display** for a session with no name — email, or "Guest".

---

## P2-5.6. Calendar sync and invitations — PARTIAL

**What we have.** One-way `.ics` invite to the booker (`GET /api/bookings/{id}/invite.ics`) and a
public read-only iCal feed for the city (`GET /api/calendar.ics`), both in `internal/calendar`.
Cancel and reschedule re-issue the invite.

**Missing.** Real sync to the municipality's calendar platform — Microsoft 365 and Google
Workspace, named in v2 §7.4. An iCal feed is a poll, not a sync: it cannot withdraw an entry, and
subscriber refresh is client-controlled. Needs OAuth credentials, event create/update/delete, and
an external-id mapping (the `IntegrationRecord` entity in v2 §6).

**Decision taken** (`Open-questions.md`): rather than committing to one platform, the admin
**selects** the calendar provider (Google Workspace, Microsoft 365, other) and the matching
integration is applied. So the deliverable is a `CalendarProvider` interface with per-provider
implementations — the same shape as the existing `PaymentProvider` interface in `internal/payment`,
which is the pattern to copy. Whether two-way sync or one-way push plus `.ics` is sufficient
follows from the selected provider.

---

## P2-5.7. Programs and registration — MISSING (entire module)

Nothing in the codebase touches this. Edmonton: ~20,000 courses, 100,000+ registrations/year.

**To build.**

- `Programme` / session / class definitions with seasons, schedules and repeating patterns, built
  from **reusable templates** (staff will not hand-enter 20,000 courses).
- Configurable registration open/close dates, with optional resident-priority windows (shares
  machinery with P2-5.5 allocation windows).
- Eligibility rules: age, date of birth, skill level, completed prerequisite. Note DOB is new
  personal data — `domain.User` holds only email, name, role, residency and address today.
- **Progression tracking** through skill levels; completion advances the participant and
  satisfies the next level's prerequisite. v2 §8 Q7 flags whether level schemes are
  municipality-defined or fixed — **this is a blocking design decision**, and municipality-defined
  is the answer consistent with v2's "rules belong in configuration" rule.
- Capacity limits with automatic waitlists and **automatic promotion** when a place is released.
  Our `internal/waitlist` is booking-specific and notify-only (it emails waiting residents when a
  slot frees); programme waitlists must _offer and hold_ the place.
- Participant records with emergency contacts and medical/accessibility notes, **visible only to
  authorised staff** — the most sensitive data in the platform, and the first thing here needing
  field-level access control.
- Instructor assignment, attendance tracking, **mobile-usable rosters**.
- Withdrawals and transfers between sessions with policy-driven refunds or account credits
  (account credit is a new financial primitive — see P2-5.11).
- **SCALE: 100+ registrations/second at opening with no oversell and no lost registrations.** Our
  proven pattern (`SELECT … FOR UPDATE` inside a transaction) is correct but serialises on the
  hot row; 500 simultaneous attempts on one 100-place course is the exact worst case for it.
  Needs a queue/reservation design, and the default SQLite driver cannot serve it at all.

**Acceptance criteria:** prerequisite refusal with explanation; automatic waitlist promotion on
withdrawal; 500 attempts on 100 places → exactly 100 succeed, rest waitlisted, none lost.

---

## P2-5.8. Memberships — MISSING (entire module)

Nothing in the codebase. Edmonton: 130,000+ members across 20+ facilities.

**To build.**

- Membership products: term, price, entitlements, and which facilities and times they admit.
- Categories — individual, family, corporate, subsidised — each with own pricing and eligibility.
- **Fee-assistance / low-income subsidy** programmes with eligibility verification and
  **discreet handling at point of sale** (the subsidised rate must apply without disclosing
  status to bystanders). **This comes from P2-5.11a** — subsidy is an entitlement type, so enrolment,
  expiry, silent re-validation and the per-type disclosure rule are inherited rather than rebuilt
  here. Discretion is a property of the entitlement model, not of this module's UI.
- One membership valid across multiple facilities, with access rules evaluated **per facility and
  per time of day**.
- Lifecycle: join, renew, freeze, transfer, cancel, with pro-rating.
- Entitlement checks applied automatically when a member books a space or registers for a
  programme — i.e. the booking and registration paths both call into the membership engine.
- **Household / named dependants** (v2 §6 expands `Customer` to carry household members). We
  have a flat `domain.User`; a family membership covering four named dependants has nowhere to
  live.

**Acceptance criteria:** off-peak membership refused at peak with drop-in fee offered; fifth
person refused under a four-dependant family membership; subsidised rate applied discreetly.

---

## P2-5.9. Admissions, retail and point of sale — MISSING (entire module)

Nothing in the codebase — and v1 assumed all payment happens on a web form
(`internal/payment` is a mock-Stripe web checkout behind a `PaymentProvider` interface).

**To build.**

- POS for counter transactions **with payment terminal hardware** and a published supported-device
  list. Hardware integration is the single largest unknown in phase 2.
- Drop-in admission with fast credential validation — **granted or refused within two seconds**
  at a busy queue.
- **Revenue splitting** across departments/partners within one transaction.
- Merchandise inventory: stock levels, receiving, adjustments, retail sale through the same till.
- Cash handling: float management, till reconciliation, daily cash-out with variance recorded.
- Counter transactions attach to the **same customer account** as online activity (feeds P2-5.13).

**Note.** v2 §8 Q2 asks build / partner / defer, and calls it out as undeferrable because it is
hardware-dependent. It is also the module least served by our current architecture: everything
we have assumes a browser session and an authenticated resident. **Recommend answering Q2 before
any phase-2 estimate is committed.**

---

## P2-5.10. Events, permits and workflow — MISSING (module in all but name)

**What we have.** Only the document-upload primitive: `internal/media` (magic-byte sniffing,
type whitelist, image re-encode, size/decompression caps, random storage name, stored outside any
web root, served with `nosniff` + locking CSP + attachment) and `internal/waiver` gating booking
confirmation on a signed waiver. That is a good foundation for permit document upload and should
be reused as-is.

**Missing.** Everything else:

- **Configurable application forms per permit/event type, built by staff without code** — a form
  builder plus dynamic form rendering and storage. No form-definition entity exists.
- **Multi-stage approval workflow** routed by department, application type or value, including
  **parallel approvals** across departments. Our approval is one staff action on one booking.
- **Document expiry tracking and renewal reminders.** `domain.WaiverDocument` stores only
  `BookingID`, stored name, content type and size — no issue date, no expiry, no reminder state.
  (`internal/reminders` already has the scheduler pattern to copy — it nudges bookers within 24h,
  idempotent via `Booking.ReminderSentAt`.)
- **Applicant-visible status tracking with internal staff notes kept separate** from applicant
  correspondence.
- **Conditions attached to an approval, acknowledged by the applicant before the permit issues.**
- **Post-event invoicing for actual costs**, reconciled against the permit and the original
  estimate.
- **Configurable service standards with escalation** when an application stalls past target.

**Acceptance criteria:** three-department approval identifies the outstanding department and
withholds the permit; unacknowledged conditions withhold the permit; over-estimate actual costs
generate a reconciled invoice.

---

## P2-5.11. Financial operations — PARTIAL

**What we have.** Fee and deposit shown before commit; mock-Stripe checkout behind a
`PaymentProvider` interface (`internal/payment`) with a `StripeProvider` slot ready;
`domain.Payment` (one current row per booking) plus an append-only `domain.PaymentTransaction`
ledger recording every charge, decline and refund with provider ref and card last-4; staff refunds
(`POST /api/staff/bookings/{id}/refund`) and a payments ledger screen (`StaffPayments.tsx`);
resident vs non-resident pricing (`Facility.FeeFor`); deposits stored (`DepositCents`); a
reporting dashboard (`internal/reports` → `StaffReports.tsx`) with revenue, bookings,
utilisation, pending/overdue, by-facility, top spaces, 6-month trend and resident split.

**Missing.**

- **A fee engine.** Pricing today is two integers on the facility — `FeeCents` and
  `NonResidentFeeCents` — a flat per-booking charge. v2 needs rates by asset, **time of day, day
  of week, season, duration** and **customer category** (non-profit, commercial). This is the
  `FeeSchedule` entity, and it must be shared by bookings, registrations, memberships and POS.
- **Externally-evaluated residency** — see §P2-5.11a below. Residency currently resolves to a single
  local boolean and is the input to `Facility.FeeFor`; it needs to become an externally-determined,
  expiring, provenance-carrying category before the fee engine is built on top of it.
- **Rate blending.** A 4pm–8pm booking crossing a 6pm rate change must charge each portion at its
  own rate. Nothing today can express this — the fee is a single lookup. Note the fixed order of
  operations: **blend across time windows → stack entitlements (P2-5.11a) → apply tax.**
- **Tax configuration per fee type with exemptions.** No tax handling anywhere; stored amounts
  are single `AmountCents` values.
- **Invoicing** for accounts billed after the fact, with payment terms and aging. Payment is
  pay-now only.
- **GL coding per fee** and an export/interface to the municipality's financial system. No GL
  concept exists. Each entitlement reduction also carries its own GL treatment —
  **discount or transfer, an admin setting per entitlement type** (P2-5.11a constraint 11) — so
  export lines derive from the stacked breakdown rather than from a single fee figure.
- **Daily reconciliation and revenue reporting with CSV and Excel export.** Verified: there is no
  export path in `internal/reports` — the dashboard is JSON to screen only. v2 wants each export
  line to carry its GL code and to balance to the day's takings.
- **An ad-hoc report builder staff can use without vendor involvement.** Our reports are
  hand-written SQL aggregations with a fixed month/quarter/year period.
- **Deposit hold and release** as a lifecycle. `DepositCents` is stored but there is no
  hold/release flow.
- **Account credits** (needed by programme withdrawals, P2-5.7).
- **Transaction expansion** (v2 §6): revenue splits, channel (online vs counter), GL posting.
- Refunds are staff-initiated; v2 wants policy-driven automatic refunds on cancellation within
  the refundable window, with the reversal reflected in the GL export.
- **Admin-selectable payment provider** (from `Open-questions.md`). Our `PaymentProvider` interface
  already abstracts the gateway, but the implementation is chosen at wire-up time in
  `cmd/server/main.go` — there is one `MockProvider` and a `StripeProvider` stub. The decision taken
  is that a new provider ships as its own module and the **admin selects** which is active, so
  provider choice must move from compile-time wiring to runtime configuration (with credentials
  handled per §7.2 — never in the repo). Refunds and deposits may be handled in-app or delegated to
  an existing municipal system, which the provider abstraction should also accommodate.
- **REVISED, and already satisfied:** no patron-facing convenience surcharge — we charge none.

At $70M/year of transactions (Edmonton), this module carries the most audit exposure of anything
in phase 2.

---

## P2-5.11a. Entitlement determination — DESIGN AGREED

Not a v2 section. A design decision taken in review (Aug 2026) that must be settled **before** the
fee engine is built, because the fee engine takes its customer category from here.

**Residency is one kind of entitlement, not its own subsystem.** The abstraction is a general
*entitlement determination*: an externally-sourced, categorised, expiring qualification carrying
provenance. Residency and §5.8's fee-assistance/low-income subsidy are the two confirmed types.

### Why

Residency today is a single local boolean, written by two different actors with no record of which
one won:

- `POST /verify-residency` (`internal/httpapi/auth.go:87`) accepts an **address from the request
  body** and sets the flag. Post any address, become a resident.
- The C2 login path honours a `residency_status` claim and treats it as authoritative over the
  self-attested value (`internal/auth/service.go:110-117,275-280`).

So self-assertion wins until the next login, the flag never expires, and nothing records where the
determination came from. `Facility.FeeFor(user.IsResident)` then prices off it
(`internal/booking/service.go:81`).

### The decision

**An entitlement is determined by a provider, never asserted by the client.** The provider is also
**discoverable**: it publishes what it needs in order to enrol and evaluate someone, so the
enrolment UI renders from that contract instead of hardcoding any municipality's rules.

- Shape it like the existing `PaymentProvider` abstraction: an `EntitlementProvider` with
  `Describe()` (the input contract — required fields, accepted evidence types, enrolment steps,
  versioned) and `Evaluate()` / `Enrol()`. Registration is **by entitlement type**, admin-selectable,
  and one provider may serve several types. C2 is the first residency implementation;
  `internal/auth/c2.go` and `internal/servicecard` are the in-repo precedents for C2
  server-to-server calls.
- **Not every provider is remote.** Subsidy eligibility may be verified by staff against an uploaded
  document rather than by an external system, so a local/staff-verified implementation must satisfy
  the same interface. Forcing every type through an HTTP adapter would be the wrong generalisation.
- **The descriptor publishes what is needed from the applicant, not what makes the check pass.**
  Publishing the passing criteria tells an attacker what to forge. A human-readable statement of
  the policy is fine — municipal residency rules are public — but the machine-readable part is the
  input schema.
- A successful enrolment returns a durable **external reference, stored provider-scoped** —
  `provider` + `ref` together, the same pattern as `Payment.Provider`/`ProviderRef`. A bare ref is
  meaningless without knowing who issued it.
- The stored determination carries **type, provenance and an expiry**: entitlement type, outcome,
  category, provider, ref, evaluated-at, valid-until. `IsResident` has none of this today, and
  people move.
- Re-validation is **silent**, using the stored ref, so a returning holder proves nothing again.
- On a negative result the **booking flow offers a proving form**, rendered from `Describe()`, to
  establish a fresh enrolment. Declining simply means normal rates — an entitlement question must
  never block a booking.

### What is and is not an entitlement

Generalising invites over-collapsing, so the boundary matters. **In scope**: residency; fee
assistance / low-income subsidy (§5.8); non-profit and commercial customer category (§5.11), where
a municipality verifies it rather than staff simply assigning it; affiliated-group status, which
§5.5's allocation priority windows need in order to decide who may book early. Insurance and waiver
currency (§5.10, which wants expiry tracking and renewal reminders) is the same shape — an
externally-evidenced qualification with a validity window — and is a strong candidate.

**Out of scope, deliberately**: membership *access rules* — which facilities and times a product
admits — are derived from a purchased product, not a verified qualification, and have their own
lifecycle (join, renew, freeze, transfer, pro-rate). Programme prerequisites (§5.7) are derived from
our own completion records, so they may consume the same gate at the point of registration but need
no provider. Collapsing either into this abstraction turns it into a god-object.

### Correctness constraints

1. **Never call a provider inside the booking transaction.** `booking.requestOne` runs inside a
   transaction holding `SELECT … FOR UPDATE` locks (`internal/booking/service.go:47,110`). An
   external HTTP call there holds row locks for the provider's latency, serialising bookings behind a
   third party and breaking under the concurrency §7.3 requires. Resolve entitlements **before** the
   transaction opens — and note that generalising multiplies this: several entitlement types mean
   several potential callouts, so resolve them together and concurrently, not serially per type.
2. **Quote and charge must use the same determinations.** Resolve once, stamp the resolved set onto
   the booking request, and have the fee calculation read *that* — not a fresh call. Otherwise a
   determination that flips between price-shown and submit charges the user a different amount than
   they agreed to. `Booking.Resident` + `FeeCents` already capture at booking time; extend them to
   record which determinations were applied. **Keep `Booking.Resident` (or a view over it) so
   `reports.residentPct` keeps working** rather than rewriting the reporting split as part of this.
3. **Three outcomes, not two.** *Expired/revoked* → proving form. *Ref unknown* (enrolment deleted,
   provider migrated) → proving form, but not worded as the user's fault. *Provider unreachable* →
   **not** a negative: serve the last-good cached determination while still within validity, and
   fall to normal rates only when no usable determination exists, with an explanation. Do **not**
   copy `internal/auditlog`'s best-effort non-blocking pattern here — that is right for audit and
   wrong for anything that sets a price.
4. **Rate is fixed at booking time.** Where proving cannot answer synchronously (document review, a
   queued lookup), a later successful enrolment applies to **subsequent** bookings rather than
   repricing a completed one. This avoids building a price-adjustment workflow on the
   `PaymentTransaction` ledger for what is a pricing question. (The alternative — refund the
   difference on approval — is mechanically possible but has to be staffed.)
5. **Switching provider invalidates every stored ref** for that type, which would silently drop all
   holders to normal rates. A provider change must force deliberate re-enrolment.
6. **Store the reference and the decision, not the evidence.** Let the provider hold the evidence;
   this keeps it out of our database and out of MFIPPA/FOIP access requests (§7.2). Where documents
   must be uploaded, reuse `internal/media` rather than inventing storage.
7. **Audit every determination** that affects price — type, provider, ref, outcome — via
   `internal/auditlog`. Never the evidence.
8. **Sensitivity is per type, and it is a property of the model — not of the UI.** §5.8 requires a
   subsidised rate to apply "without disclosing their status to bystanders". Residency is fine to
   surface ("resident rate applied"); subsidy is not, and a generic "entitlements applied: …" display
   would leak it onto a counter-facing screen or a receipt line. So each entitlement type carries a
   disclosure attribute that the fee breakdown, receipts, POS display and staff views all honour.
   This constraint only becomes visible once the abstraction is shared — it is the main hazard the
   generalisation introduces, and it would otherwise ship as a privacy defect.
9. **Entitlements stack, in a configured order: resident discount first, then subsidy.** Someone may
   hold several at once, and each applies in turn to the **running** amount rather than one winning
   outright. So entitlement types carry an ordering, and the fee engine applies them as an ordered
   reduction, not a lookup. Four things follow and must be pinned down with it:

   - **Order of operations against the rest of the fee engine.** Base rate is **blended across time
     windows first** (§5.11's 4pm–8pm-crossing-6pm case), *then* entitlements stack onto the blended
     total, *then* tax applies. Discounting before blending gives a different answer wherever a
     reduction is a fixed amount rather than a percentage, so the order has to be fixed, not
     incidental.
   - **Rounding is defined per step.** Money is integer cents throughout (`FeeCents`,
     `AmountCents`, `DepositCents` — the codebase is deliberate about avoiding float). Two sequential
     reductions require a rounding decision at each step, and rounding per step versus once at the
     end produces different totals. Pick one rule, apply it at every step, and **persist the
     intermediate amounts** — staff will be asked why a booking came to $47.63.
   - **Floor at zero, never negative.** Stacked reductions can exceed the base rate.
   - **Itemisation collides with constraint 8.** A stacked breakdown naturally reads "Resident rate
     −$10.00, Subsidy −$20.00" — which discloses subsidy status on exactly the counter-facing screen
     and printed receipt §5.8 says it must not appear on. Resolution: the **internal** record
     itemises every step, for audit and GL; the **patron-facing** rendering must be able to collapse
     non-disclosable steps into a single total or a neutral label. This is a direct consequence of
     combining stacking with discretion, and it is the sort of thing found at a POS demo rather than
     in a unit test.

10. **A charge stacked to zero skips payment entirely and writes no payment ledger row.** No provider
    call, no `Payment` row, no `PaymentTransaction`. This needs no new machinery: `Payment` rows are
    only ever created by the `Pay` path (`internal/payment/payment.go:76`), `Booking.Payment` is
    already a nil-able pointer, and the reports dashboard's "overdue" figure is `PendingOver24h` —
    pending *approval* age, not payment state — so an uncharged booking is not miscounted
    (`internal/reports/reports.go:103-109`). Two things still have to hold:

    - **A booking discounted to zero must stay distinguishable from a free asset.** Both end up with
      no payment record, so the difference lives only in the entitlement determinations stamped on
      the booking. That capture is therefore **load-bearing for reporting, not just for audit** — it
      is the only thing that can answer "how much fee assistance did we provide this year?"
    - **Skipping *payment* is not the same as skipping the *GL*.** Under transfer treatment
      (constraint 11) a zero-charge subsidised booking still moves money between accounts and still
      generates a GL line, even though the patron paid nothing. "Avoids the ledger" applies to the
      payment ledger, which records gateway interactions — and there were none.

11. **GL treatment of an entitlement is an admin setting, configured per entitlement type.** Whether
    a reduction is a **discount** (revenue is simply reduced) or a **transfer** (the reduced portion
    posts to a named subsidy or grant account) is municipal finance policy, so it is configuration,
    not code. Per type rather than global: a resident discount is a rate difference and is almost
    always a discount, while subsidy is the one that plausibly needs transfer treatment. Three
    constraints come with it:

    - **Transfer mode requires an offset GL account**, so the setting is a mode *plus* an account
      code, not a boolean. Selecting transfer without a configured account must be rejected at
      configuration time — otherwise §5.11's "the export balances to the day's takings" criterion
      fails at export time instead.
    - **The treatment in force is captured at transaction time**, exactly as rates and determinations
      are. Changing the setting must never retroactively rewrite historical postings.
    - It is a configuration change, so it inherits §P2-7.2's versioning and attribution.

### Knock-on effects

- Residency stops being a boolean, which the fee engine needs anyway for §5.11's non-profit and
  commercial rates. Sharing one abstraction with §5.8's subsidy eligibility means it is built once
  instead of two or three times — this is the reason the general form was chosen.
- **§5.8 memberships gets its subsidy machinery from here**, so that module inherits enrolment,
  expiry, silent re-validation and discreet handling rather than implementing them again. Membership
  *access rules* stay separate (see "What is and is not an entitlement" above).
- **§5.5 allocation priority windows becomes a consumer**, not a separate feature: "affiliated or
  resident groups may book before general release" is an entitlement check on a booking window.
- It removes the guest self-assertion hole noted in P2-5.5a: with residency externally determined,
  `verify-residency` stops being a client-writable endpoint and becomes "begin an evaluation". A
  guest with no prior enrolment simply has no ref, so they go to the proving form or normal rates.
- The fee engine's interface changes shape: `Facility.FeeFor(isResident bool)` gives way to a rate
  resolved from the **set** of active entitlements plus asset, time and duration. Worth landing
  before the fee engine, not after.

---

## P2-5.12. Staff back-office — PARTIAL

**What we have.** Facility create/edit/delete + blackout management (`StaffFacilities.tsx`),
approval queue (`StaffQueue.tsx`), payments ledger, reports, user/role admin (`StaffUsers.tsx`),
audit log viewer (`StaffAudit.tsx`). Three global roles — resident / staff / admin
(`domain.Role`) — enforced by `auth.RequireRole` middleware in
`internal/httpapi/router.go:109,127`.

**Missing.**

- **Department-scoped permissions.** Roles are global: any `staff` user can approve any booking
  and edit any facility. v2 requires staff to see only their own service area — needs a
  department dimension on users and assets, and scoping in the service layer (not just the UI).
- **Bulk import and edit** for assets, customers, schedules and programmes — with validation and
  an error report, not a silent partial import.
- **A staff dashboard** summarising pending approvals, today's bookings and exceptions needing
  attention. We have a pending-approvals queue and an analytics dashboard; neither is the
  at-a-glance operational view v2 describes.
- **A training environment with representative data.** Our `internal/seed` package already
  generates a deterministic year of history (7 facilities, ~600 bookings, payments) — good raw
  material, but there is no separate training environment to point it at.
- **Module enablement** (carried over from v2 §4, and §2.1's goal of adopting the platform one
  module at a time). A configurable enabled-modules set, enforced **server-side** — routes refuse
  when a module is off — and reflected in SPA navigation. Server-side is not optional: hiding a nav
  link is not access control. This is what lets the municipality take booking now and add
  registration or memberships later without a fork.
- No cross-asset master schedule view for staff.

---

## P2-5.13. My account — PARTIAL

**What we have.** `MyBookings.tsx` + `/api/bookings/mine` and `/api/waitlist/mine`: bookings and
waitlist entries, with self-service cancel and reschedule.

**Missing.** The unified account v2 describes — programme registrations, memberships, permits and
**payment history across both online and counter channels** in one place. Blocked on P2-5.7,
5.8, 5.9, 5.10. Cancellation/modification policy windows are also hardcoded (owner may change an
active booking before it starts) rather than configurable.

---

## P2-5.14. Notifications and reminders — PARTIAL

**What we have.** `notify.Notifier` interface with **one implementation: `LogNotifier`**
(`internal/notify/notify.go:30`) — it writes to the log. Verified: there is no SMTP client and no
`FB_SMTP_*` config. Reminder scheduling works (`internal/reminders`, idempotent via
`Booking.ReminderSentAt`) and reminders carry access instructions, as v2 requires. Booking
submitted / confirmed / denied / cancelled / reminder / waitlist-opened events all fire.

**Missing.**

- **Actual email delivery.** For a sales demo the log notifier is fine; for v2 it is the gap
  between "notifies" and "notified".
- **SMS as a configurable option.**
- **Configurable templates** with municipal wording and branding (carried over from v2 §4).

---

## P2-5.15. Further capabilities

| Capability                                             | Verdict                                                                                                                                                                                                                                                                           |
| :----------------------------------------------------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Map view of assets + directions                        | **DONE** — `web/src/pages/MapView.tsx`, Leaflet + OSM, facility lat/long                                                                                                                                                                                                          |
| Waitlists on popular assets with notification          | **DONE** — `internal/waitlist`                                                                                                                                                                                                                                                    |
| Public read-only availability without an account       | **DONE** — `/availability`, `/api/facilities/{id}/calendar`                                                                                                                                                                                                                       |
| Utilisation and revenue reporting                      | **DONE** — `internal/reports`                                                                                                                                                                                                                                                     |
| **Demand** reporting                                   | **PARTIAL** — utilisation and revenue only; nothing measures unmet demand (searches with no result, waitlist depth, denied requests)                                                                                                                                              |
| Bilingual EN/FR — UI                                   | **DONE** — `react-i18next`, `web/src/lib/i18n.ts`, every page via `t()`                                                                                                                                                                                                            |
| Bilingual EN/FR — **facility content**                 | **MISSING, and now confirmed in scope.** `Open-questions.md` commits to the admin entering facility name and description **in both languages**, displayed per the user's preference. Today these are one `varchar`/`text` per field on `domain.Facility` — so this is a schema change plus admin-form and read-path work, not a translation-file exercise. |
| **Configurable terminology**                           | **MISSING** — carried over from v2 §4: local naming (a slip, a berth, a diamond, a pad). Interacts with i18n — `web/src/lib/i18n.ts` holds static EN/FR bundles, so overrides must layer on top of the `t()` lookups rather than replace them.                                     |
| **City branding / design match**                       | **MISSING** — `Open-questions.md` commits to matching existing city branding. The SPA has a fixed Tailwind theme and "Rivermont Spaces" naming; needs logo, colour tokens and wordmark driven from configuration rather than hardcoded. Single-city scope now that tenancy is descoped. |
| **Website embedding** of availability and registration | **MISSING** — needs embeddable widgets, a framing policy that isn't `X-Frame-Options: DENY` (currently set globally in `internal/httpapi/security.go`), and a configurable CSP `frame-ancestors` allowlist                                                                        |

---

## P2-6. Data model deltas

New entities from v2 §6, none of which exist today:

| Entity              | Notes                                                                       |
| :------------------ | :-------------------------------------------------------------------------- |
| `Programme`         | Course, session, schedule, capacity, eligibility, prerequisites, instructor |
| `Registration`      | Participant, programme, status, attendance, progression, waitlist position  |
| `Membership`        | Product, category, term, entitlements, access rules, subsidy status         |
| `PermitApplication` | Form data, documents, approval stages, conditions, decisions, costs         |
| `FeeSchedule`       | Rate rules by asset/time/season/category; tax treatment; GL codes           |
| `InventoryItem`     | Merchandise, stock level, receiving and adjustment history                  |
| `IntegrationRecord` | External identifiers linking bookings, calendars and financial postings     |

Expanded existing entities:

| Entity                         | Expansion needed                                                                                                            |
| :----------------------------- | :-------------------------------------------------------------------------------------------------------------------------- |
| `Facility` → `Asset`           | Hierarchy level + parent semantics, station division, `BookableDirectly`, richer accessibility, multiple media, floor plans |
| `User` → `Customer`            | Household / named dependants, DOB, unified cross-channel history; keep the C2 `sub` link; **residency as a provider-scoped determination** (provider + ref + category + evaluated-at + valid-until) replacing the `IsResident` boolean — see P2-5.11a |
| `Payment`/`PaymentTransaction` | Revenue splits, channel (online/counter), GL posting, tax lines, deposits held/released, account credits                    |
| `User and role`                | Department scope on staff accounts                                                                                          |
| `AuditLog`                     | Extend beyond bookings to permits, refunds **and configuration changes**                                                    |

**Naming.** v2 consistently says _asset_, not _facility_, and _customer_, not _user_. Renaming
`domain.Facility` → `domain.Asset` is a wide but mechanical change; doing it at the start of phase
2 is cheaper than living with two vocabularies across five modules.

---

## P2-7. Non-functional

### 7.1 Accessibility — PARTIAL

**Have:** `<html lang>` synced to the EN/FR toggle, skip-to-content link, `main`/`nav` landmarks,
focus-visible rings on interactive controls, `role="alert"`/`status` live regions on errors and
spinners, accessible names on every control (verified: no unnamed buttons/inputs, no images
without alt), slate-500 floor on informational text.

**Missing:** full WCAG 2.1 AA contrast audit; mobile pass; **staff functions usable on a tablet**
(the staff tables are desktop-shaped today); AODA + provincial-equivalent alignment; a
**published accessibility conformance report** available on request.

### 7.2 Security and privacy — PARTIAL

**Have:** OIDC login against C2 with local sessions and roles; HTTP-only session cookies;
server-side authorization via `RequireAuth`/`RequireRole`; security headers on every API response
(`nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`, `default-src 'none'` CSP, HSTS when
`FB_ENV=prod`); hardened media upload (`internal/media`); an **append-only, tamper-evident
hash-chained audit log** via the central audit-logging service (`internal/auditlog`) — which
substantially answers v2's immutable-audit requirement for booking actions.

**Missing:**

- **Configurable SSO.** Our identity provider is C2, wired via `FB_OIDC_*` process env
  (`internal/auth`). v2 needs staff SSO against _the municipality's own_ IdP, i.e. configurable
  OIDC/SAML rather than a hardcoded C2 dependency.
- **MFA on all administrative accounts** — currently delegated entirely to C2 and unverified here.
- **PCI-DSS compliant payment handling with no card data stored.** We store `CardLast4` on
  `PaymentTransaction` (permitted, but needs a formal scope assessment) and the provider is a
  mock. Real compliance starts with answering v2 §8 Q5 (which processor).
- **Canadian data residency** for all customer data — a hosting decision (v2 §8 Q6); today the app
  deploys to a single shared host (`deploy.md`).
- **Named sub-processor list** with advance notice of change.
- **MFIPPA / FOIP compliance**, including **support for access and correction requests** — this is
  a feature (export/correct one customer's data on request), not just a policy.
- **Encryption at rest** — not addressed; the default is a local SQLite file.
- **Annual third-party penetration testing** with a client-available summary.
- **Security incident detection and notification** within a defined period.
- **Documented backup, retention and DR with stated RPO/RTO.**
- Audit coverage must extend to permits, refunds and **configuration changes** — and configuration
  changes specifically must be **versioned, attributable and rollback-able** (carried over from v2
  §4). `domain.AuditLog` + `internal/auditlog` cover booking actions today; config changes are not
  captured at all.

### 7.3 Performance and scale — MISSING

v2's numbers are architecture constraints: **10,000 concurrent users; 100+ transactions/second at
registration peak; 8,000+ assets; 130,000+ member records** with no search or listing degradation.

Current state, concretely:

- **Default DB is a single-file pure-Go SQLite** (`FB_DB_DRIVER=sqlite`), chosen for portability.
  MariaDB is supported but is not the default. Neither is configured for horizontal scale.
- **No pagination anywhere.** `facility.Service.List` returns all matching rows with accessories
  preloaded; `bookings/mine`, `staff/bookings/pending` and `ListForCalendar` are likewise
  unbounded.
- **Reports run full-table aggregations** per request (`internal/reports`) with no caching or
  pre-aggregation.
- **No caching layer, no read replicas, no queue.** Single process, single instance.
- The `FOR UPDATE` booking lock is correct but serialises on the contended asset — the wrong shape
  for a 100/second registration surge (see P2-5.7).
- **No load or soak test evidence**, which v2 §7.3 requires be available to prospective clients.

v2 helpfully notes these optimisations should be **deferred until a large client is genuinely in
view** — but the cheap, non-deferrable parts are pagination, indexing and not defaulting to
SQLite. Do those; defer the rest.

### 7.4 Integration and data portability — MISSING

- **Documented, versioned public REST API covering all core entities, treated as a product
  surface.** We have an internal JSON API for our own SPA: unversioned (`/api/...`, no `/v1`), no
  OpenAPI spec, no published documentation, and session-cookie auth only — no API keys or tokens
  for third-party callers. (`CLAUDE.md`'s mention of personal access tokens is aspirational; the
  router has none.)
- **Data migration tooling** to import customers, assets, bookings and history from a legacy
  system, **with validation and reconciliation reporting before cutover**.
- **Full customer data export in a non-proprietary format, on demand.**
- **Transition assistance on exit at published rates. No lock-in.** (PROCESS.)
- **Financial system integration** for GL postings and receivables (see P2-5.11).
- **Calendar integration with Microsoft 365 and Google Workspace** (see P2-5.6).
- **A pluggable integration interface** (from `Open-questions.md`). The decision taken is that
  integrations to municipal systems — permit, finance, staff SSO, **residency evaluation
  (P2-5.11a)**, calendar (P2-5.6), payments (P2-5.11), and the CRM push on approval (P2-5.5) — are
  implemented against a common interface and selected by the admin, rather than each being bespoke.
  `internal/payment`'s `PaymentProvider` and `internal/auditlog`'s best-effort, non-blocking
  recorder are the two existing patterns to generalise from — but note they sit at opposite ends of
  a spectrum, and each integration must pick deliberately. Residency is the one where an outage has
  a **pricing** consequence, so it needs the cached last-good behaviour of P2-5.11a rather than
  plain best-effort. Note v2 §2.2 excludes *being* a CRM; pushing to one is an integration, not a
  contradiction.

### 7.5 Service operations — PROCESS

SLA with a stated, measured uptime target; support model with severity levels and response and
resolution targets; published maintenance windows; a version and patch register per component; a
stated support lifecycle; published roadmap and release notes; an implementation methodology
supporting phased delivery by service area alongside existing systems.

None of these are code, but every one is a deliverable a municipal procurement will score. The
`/healthz` endpoint is the only piece of operational surface that exists today.

---

## P2-8. Decisions

### Already taken — from `Open-questions.md`

These are settled and have been folded into the sections above. Several **add** scope this analysis
would not otherwise have carried, so they are called out rather than buried:

| Decision                                                                       | Lands in       | Adds scope? |
| :----------------------------------------------------------------------------- | :------------- | :---------- |
| Admin **selects** the calendar provider (Google / M365 / other)                 | P2-5.6         | Yes — a provider interface, not one integration |
| **Guest booking with email verification**                                        | P2-5.5a        | **Yes — new capability; all booking is behind auth today** |
| Payment provider **pluggable and admin-selectable**; refunds/deposits in-app or delegated | P2-5.11 | Yes — provider choice moves from compile-time to runtime |
| Approval structure **admin-configurable** (single / per-facility / per-department), optional CRM push | P2-5.5, P2-5.12 | Yes — CRM push is a new integration surface |
| Bilingual **facility content**, entered per language by the admin               | P2-5.15        | **Yes — a schema change, previously flagged only as a caveat** |
| City **branding/design match**                                                  | P2-5.15        | Yes — configuration-driven theming |
| **Pluggable integration interface** for permit / finance / SSO systems           | P2-7.4         | Yes — a generalised interface |
| Booking-rules engine configurable **per facility and per asset type**            | P2-5.2         | Partly — min/max/buffer exist; cancellation windows and asset-type defaults do not |

Two of these overlap v2's own open questions: the payment answer partly settles v2 §8 Q5 (the
processor half — cloud region is still open), and multi-language is settled.

### Taken in design review, Aug 2026

Decisions from the review session that go beyond what `Open-questions.md` recorded. Each is written
up where it applies rather than summarised here:

| Decision                                                                                  | Written up in |
| :---------------------------------------------------------------------------------------- | :------------ |
| Multi-tenancy **descoped**; stand-alone §4 items rehomed                                   | P2-4          |
| Staging → production **configuration promotion not required**; config is made where it applies | P2-4, P2-7.2 |
| Guest booking as a **real session** + magic-link reclaim; `RequireAuth` splits into two tiers | P2-5.5a    |
| Residency **externally evaluated** by a discoverable adapter, replacing the local boolean   | P2-5.11a      |
| Built as a **general entitlement determination** shared with §5.8 subsidy, not residency-specific | P2-5.11a |
| Residency re-validated **silently** from a provider-scoped reference; proving form inline in the booking flow on a negative | P2-5.11a |
| Rate **fixed at booking time**; a later enrolment applies to subsequent bookings only      | P2-5.11a      |
| Adapter **unreachable ≠ not a resident** — serve last-good cached determination            | P2-5.11a      |
| Entitlements **stack in a configured order** — resident discount, then subsidy              | P2-5.11a, P2-5.11 |
| A charge stacked to **zero skips payment** and writes no payment-ledger row                 | P2-5.11a      |
| **Discount vs transfer** GL treatment is an admin setting, per entitlement type              | P2-5.11a, P2-5.11 |

Two points deliberately left open by that session:

1. **When silent re-validation fires** — per session, or on entering the booking flow when the
   cached determination has lapsed. The write-up assumes the latter (revalidate when the price is
   about to matter, before the booking transaction opens); per-session is staler, per-request is
   chatty.
2. **Guest session lifetime** — see P2-5.5a.

Both remaining points are now answered — a zero charge skips payment and the payment ledger
(P2-5.11a constraint 10), and discount-versus-transfer is an admin setting per entitlement type
(constraint 11).

### Still open — these gate estimating

v2 §8 raises eight questions. Four still gate phase-2 work and should be answered before committing
dates:

1. **Q1 — Which municipality size is the design target?** A town of 20,000 and a city of
   1,000,000 differ by two orders of magnitude. This decides how much of §7.3 is real work now
   versus deferred.
2. **Q2 — POS: build, partner or defer?** Hardware-dependent, mandatory for recreation buyers,
   and the module furthest from our current architecture. Undeferrable per v2.
3. **Q3 — Which modules are in the first release?** v2 suggests booking + financial operations is
   the smallest genuinely sellable combination. That matches our position: booking is largely
   built, so financial operations is the highest-leverage next module.
4. **Q7 — How is programme progression modelled?** Municipality-defined level schemes versus a
   fixed model changes the shape of the registration data model, and registration is the largest
   new module.

Q5's remaining half (cloud region) and Q6 (Canadian residency in all cases) gate all of §7.2 — no
security documentation can be written until they are answered.

---

## P2-9. Suggested sequencing

Five modules plus a scale story is not one phase. A defensible split, ordered so nothing has to be
built twice:

**Phase 2a — Foundations (fix the correctness gap; rename before five modules build on the old
vocabulary).** `Facility` → `Asset` rename + hierarchy semantics · **hierarchy-aware conflict
detection** (the outstanding correctness gap) · pagination and indexing (P2-7.3 cheap parts) · the
remaining search filters (P2-5.3) · real email delivery (P2-5.14).

**Phase 2b — Financial operations (v2 §8 Q3's other half of a sellable release).**
**Entitlement determination (P2-5.11a) first — the fee engine takes its customer category from it,
and §5.8 memberships later inherits its subsidy machinery from it** · fee engine + rate blending ·
tax · GL coding + export · invoicing and aging · deposits hold/release · account credits · CSV/Excel
export + reconciliation.

**Phase 2c — Programs and registration.** The largest new module; needs 2b's fee engine, and
forces the scale question.

**Phase 2d — Memberships.** Depends on households (2a data model) and the fee engine (2b);
entitlement checks then hook into booking and registration.

**Phase 2e — Events and permits.** Reuses `internal/media` and the audit trail; adds the form
builder and multi-stage workflow.

**Phase 2f — Admissions, retail and POS.** Gated on v2 §8 Q2. Hardware-dependent and the least
served by the current architecture.

**Throughout:** back-office depth (P2-5.12), accessibility completion (P2-7.1), the public
versioned API and migration tooling (P2-7.4), and the §7.5 process deliverables.

**One caveat worth stating plainly:** the current build is a sales demo. Several of its
deliberate demo shortcuts — mock payments, log-only notifications, default SQLite, seeded data —
are exactly the things v2 turns into hard requirements. Phase 2 is not only additive; it also
converts the demo's simulations into real integrations.

**Open questions for the developer**

Decisions that shape scope and effort, worth settling before build:

- Which calendar platform does the municipality use (Google Workspace, Microsoft 365/Outlook, other), and is two-way sync required or is a one-way push plus .ics invite enough for v1?

Answer: Can we create an admin option allowing them to select, and then we can implement the appropriate integration based on that selection.

- Do residents need accounts, or can they book as guests with email verification? How is residency verified for resident pricing?

Answer: We can implement a guest booking option with email verification, and for residency verification, we can use a simple address verification process or integrate with a municipal database if available with the C2 adapter.

- Which payment provider, and does the city need refunds/deposits handled in-app or through an existing system?

Answer: We can integrate with a payment provider like Stripe or PayPal, and we can handle refunds/deposits in-app or through an existing system based on the municipality's preference. We should build payment as a pluggable interface so that we can support multiple providers if needed, and allow the admin to select the provider they want to use. The system should also support refunds and deposits, and we can implement the appropriate logic based on the selected payment provider.

The system should allow a new payment module to be created and included that covers a difference in the payment provider, and allow the admin to select the provider they want to use.

- How is staff approval structured — one approver, per-facility approvers, or by department?

Answer: We can implement a flexible approval workflow that allows for one approver, per-facility approvers, or department-based approvers. The admin can configure the approval structure based on their needs. The system should also allow for a client to send the approval out to a CRM system, and we can implement the appropriate integration based on the municipality's preference.

- Are both official languages required at launch, and does existing city branding/design need to be matched?

Answer: Multiple language support can be implemented, and we can ensure that the system matches the existing city branding/design. We can implement a language toggle for the user interface, and we can also allow for custom branding to be applied to the system based on the municipality's requirements. In the admin interface when creating a new facility, we can allow the admin to enter the facility name and description in both languages, and we can display the appropriate language based on the user's preference.

- Does this need to integrate with any existing municipal system (e.g. a permit or finance system, or single sign-on for staff)?

Answer: We can implement integrations with existing municipal systems as needed. We can create a pluggable integration interface that allows for different types of integrations to be implemented based on the municipality's requirements. For example, we can integrate with a permit system, finance system, or single sign-on for staff based on the municipality's needs.

- What are the facility booking rules today (min/max duration, buffer, cancellation windows) that the app must enforce?

Answer: We can implement a flexible booking rules engine that allows the admin to configure the min/max duration, buffer times, and cancellation windows for each facility. The system should enforce these rules during the booking process and provide appropriate feedback to the user if their booking does not comply with the rules. The admin can also set different rules for different facilities or asset types as needed.
