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

---

**Round 2 — design review, August 2026**

Raised while reviewing PRD v2.0 (`requirements-vers-2.md`) against the current build. Full detail and the code references behind each answer live in `features-phase-2.md`; section numbers below point there.

- Is the platform multi-tenant — one deployment serving many municipalities?

Answer: No. Multi-tenancy (v2 §4) is out of scope as of August 2026. This stays a single-municipality deployment: no tenant entity, no per-request tenant resolution, no per-tenant branding, and no configuration promotion between per-tenant environments. The §4 items that stand alone for a single municipality were kept rather than dropped — module enablement, a training environment, configurable notification templates, configurable terminology, configurable SSO against the city's own IdP, and versioned/reversible configuration changes. → §P2-4.

- How does guest booking work, and how does a guest recover their booking later?

Answer: "Continue as guest" at the login prompt creates a real logged-in session backed by a real user row (guest role, with a synthetic subject of `guest:<uuid>`). That keeps one authorization model: existing ownership checks and the non-optional `Booking.UserID` keep working unchanged. The alternative — a nullable booking owner plus a guest email column — would fork every ownership check into "owned by user OR matches guest email", which is the shape that produces IDOR bugs.

The **verified email**, not the guest identifier, is the durable identity and the key to reclaiming bookings. Reclaiming from a new device sends a short-lived, single-use link, stored hashed. A guest identifier that could itself reclaim bookings would be password-equivalent — it reads and cancels bookings and downloads waivers and insurance documents — and would leak through browser history, shared links and server logs.

One prerequisite: `RequireAuth` currently means "any session", so it must split into "any session including guest" and "real account only", with every existing route explicitly assigned, **before** guest sessions ship. Otherwise guests silently gain access to every authenticated route. → §P2-5.5a.

- How is residency determined for resident pricing?

Answer: Externally, by an adapter — never asserted by the client. (Today `POST /verify-residency` accepts an address from the request body and sets the flag, so anyone can self-declare residency.) The adapter is also **discoverable**: it publishes what it needs in order to enrol and evaluate someone, so the enrolment form renders from that contract rather than hardcoding any municipality's rules in our code. C2 is the first implementation.

One boundary to hold: the descriptor publishes what is needed *from the applicant* — required fields, accepted evidence, enrolment steps — not what makes the check pass. Publishing the passing criteria tells someone what to forge. A human-readable statement of the policy is fine, since municipal residency rules are public. → §P2-5.11a.

- How is a previously-verified resident re-checked, and what happens if they no longer qualify?

Answer: A successful enrolment returns a durable external reference, stored provider-scoped (provider + reference together, so it stays meaningful if the provider changes). During a session that reference is used to re-validate **silently**, so a returning resident proves nothing again. On a negative result the booking flow offers a proving form — rendered from the adapter's descriptor — to establish a fresh enrolment. Declining simply means normal rates; a residency question never blocks a booking.

Three qualifications:

1. An **unreachable adapter is not a negative.** Serve the last good cached determination while it is still within its validity, and fall to normal rates only when no usable determination exists. Otherwise a provider outage silently reprices every resident booking.
2. The callout must happen **before the booking transaction opens.** That transaction holds row locks to prevent double-booking, and an external HTTP call inside it would hold those locks for the adapter's latency. Resolve residency first, then stamp the determination onto the booking so the price quoted is the price charged.
3. The **rate is fixed at booking time.** Where proving cannot answer synchronously — document review, a queued lookup — a later successful enrolment applies to subsequent bookings rather than repricing a completed one. This avoids building a price-adjustment workflow for what is a pricing question.

→ §P2-5.11a.

- Is the residency check built as a residency-specific provider, or as a general entitlement determination shared with membership subsidy eligibility?

Answer: A **general entitlement determination**. Residency is one entitlement type; §5.8's fee-assistance / low-income subsidy is another. Both are externally sourced, categorised, expiring qualifications carrying provenance, so they share one provider interface, one enrolment and proving flow, one expiry and silent-re-validation mechanism, and one audit path. Memberships then inherit their subsidy machinery instead of reimplementing it, and §5.5's allocation priority windows ("affiliated or resident groups may book before general release") becomes a consumer of the same check rather than a separate feature.

Two things this changes that a residency-only design would not have surfaced:

1. **Discretion becomes a property of the model, not of a screen.** Residency is fine to display ("resident rate applied"); subsidy must not be announced to bystanders. A generic "entitlements applied: …" line would leak it onto a counter display or a receipt. So each entitlement type carries a disclosure attribute that fee breakdowns, receipts, POS and staff views all honour.
2. **Not every provider is remote.** Subsidy may be verified by staff against an uploaded document rather than by an external system, so a local/staff-verified implementation has to satisfy the same interface. Forcing every type through an HTTP adapter would be the wrong generalisation.

Membership *access rules* — which facilities and times a purchased product admits — stay outside this abstraction: they derive from a purchase, not a verified qualification, and have their own lifecycle. Programme prerequisites likewise derive from our own completion records and need no provider. → §P2-5.11a.

- With multi-tenancy descoped, does the deployment still need staging → production configuration promotion?

Answer: No. v2 §4 required promoting configuration between per-tenant environments without a software release; that requirement goes with multi-tenancy. Configuration changes still need to be **versioned, attributable and reversible** within the single deployment (§P2-7.2), and staff still need a **training environment with representative data** (§P2-5.12) — neither of which depends on cross-environment promotion. Configuration is made in the environment where it applies.

- If someone holds more than one entitlement, do they stack or does the best single rate win?

Answer: They **stack, in a configured order — resident discount first, then subsidy.** Each applies in turn to the running amount, so entitlement types carry an ordering and the fee engine applies them as an ordered reduction rather than a single lookup.

Four things this pins down:

1. **Order of operations.** The base rate is blended across time windows first (a 4pm–8pm booking crossing a 6pm rate change), *then* entitlements stack onto the blended total, *then* tax applies. Discounting before blending gives a different answer wherever a reduction is a fixed amount rather than a percentage, so this order is fixed rather than incidental.
2. **Rounding is defined per step.** Amounts are integer cents throughout. Two sequential reductions need a rounding rule at each step, and rounding per step versus once at the end produces different totals — so one rule, applied at every step, with the intermediate amounts persisted. Staff will be asked why a booking came to $47.63.
3. **Floor at zero.** Stacked reductions can exceed the base rate; the result must never go negative.
4. **Itemisation has to respect discretion.** A stacked breakdown naturally reads "Resident rate −$10.00, Subsidy −$20.00" — which discloses subsidy status on exactly the counter screen and printed receipt it must not appear on. So the internal record itemises every step for audit and GL, while the patron-facing rendering can collapse non-disclosable steps into a single total or neutral label.

→ §P2-5.11a.

- If a stacked reduction brings a charge to zero, does it still go through payment?

Answer: No. A zero charge **skips payment entirely and writes no payment-ledger row** — no provider call, no payment record, no ledger transaction. This needs no new machinery: payment records are only ever created by the pay path, a booking's payment is already an optional association, and the reports dashboard's "overdue" figure counts pending *approval* age rather than payment state, so an uncharged booking is not miscounted anywhere.

Two things still have to hold:

1. **A booking discounted to zero must stay distinguishable from a genuinely free asset.** Both end with no payment record, so the difference lives only in the entitlement determinations stamped on the booking. That capture is therefore load-bearing for reporting, not just for audit — it is the only thing that can answer "how much fee assistance did we provide this year?"
2. **Skipping payment is not the same as skipping the GL.** Under transfer treatment (next question) a zero-charge subsidised booking still moves money between accounts and still produces a GL line, even though the patron paid nothing. Avoiding the ledger applies to the *payment* ledger, which records gateway interactions — and there were none.

- Is a subsidy a discount (revenue reduced) or a transfer (posted to a subsidy account)?

Answer: **Either — it is an admin setting, configured per entitlement type.** Per type rather than globally, because a resident discount is a rate difference and is almost always a discount, whereas subsidy is the one that plausibly needs transfer treatment.

Three constraints come with it:

1. **Transfer mode requires an offset GL account**, so the setting is a mode *plus* an account code, not a checkbox. Choosing transfer without a configured account must be rejected when the setting is saved — otherwise the failure surfaces later as a GL export that does not balance to the day's takings.
2. **The treatment in force is captured at transaction time**, exactly as rates and determinations are. Changing the setting must never retroactively rewrite historical postings.
3. Being a configuration change, it inherits the versioning and attribution requirement — who changed it, when, and reversible.

→ §P2-5.11a constraints 10 and 11.

---

**Round 3 — §7 confirmation pass, 13 August 2026**

Decided by jamie@dev-pro.ca on 2026-08-13, working FAC-14. This round confirms or narrows the
original §7 answers above; where a Round 3 answer conflicted with an earlier one, the conflict was
raised explicitly and the resolution is recorded here. Earlier answers are left in place rather
than rewritten, so the history stays readable.

- **Calendar platform** — *Confirmed, and now built.* The admin-selectable approach from Round 1 is
  implemented: a `calendar.Provider` interface with a module registry (`ics`, `none`, `google`,
  `microsoft`) and an admin form at `/staff/calendar`. Selecting an unbuilt two-way module records
  the municipality's decision while the app keeps running the one-way default. FAC-6 narrows to
  implementing the Google and Microsoft modules against that interface.

- **Payment provider** — *Pluggable and admin-selectable, Stripe as the first real module.* Round 3
  initially answered "Stripe directly"; that conflicted with Round 1's explicit request for a
  pluggable interface with admin selection, and Round 1 stands. FAC-2 is therefore the calendar
  pattern applied to payments — a module registry plus admin form — with `StripeProvider` as the
  first implementation and the existing mock as the zero-config default. Refunds and deposits are
  handled **in-app**.

- **Booking and cancellation rules** — *Per facility, with a municipality-wide default.* No
  hardcoded values: staff enter the real durations, buffers and cancellation windows per facility,
  falling back to a city-wide policy. An ice arena and a meeting room do not share a cancellation
  window. FAC-3 builds the Policy entity this way.

- **Staff approval structure** — *Per-facility approvers.* Each facility names the staff who approve
  for it; FAC-5 routes the "needs approval" notification to those people and FAC-10's conditional
  approvals follow the same routing. This narrows Round 1's "one, per-facility, or by department,
  admin-configurable" to the per-facility case.

- **Languages and branding** — *Both official languages at launch, with city branding applied.*
  FAC-12 (bilingual facility content, not just UI) becomes launch-blocking. Branding means replacing
  the Rivermont demo palette and wordmark, which must land **before** FAC-11's contrast audit — the
  audit is only meaningful against the final colours.

- **Resident accounts and residency verification** — *Round 2's decision stands: external adapter,
  never client-asserted.* Round 3 initially answered "self-declared address (current)", which
  contradicted §P2-5.11a's finding that today's `POST /verify-residency` lets anyone self-declare
  residency. On review the Round 2 design holds: residency (and entitlements generally) is
  determined by a discoverable external adapter, with guest sessions per §P2-5.5a. The
  `RequireAuth` split — "any session including guest" vs "real account only" — is a **prerequisite**
  and must ship before guest sessions, or guests silently gain every authenticated route.

- **Municipal system integrations** — *All three are needed:* finance/accounting export, permit
  system, and staff SSO against the city's own directory (separate from resident login via C2).
  Each is tracked as its own ticket; the pluggable integration interface from Round 1 is the shape.

**Scope note (updated 2026-08-13):** the first FAC backlog (FAC-1 … FAC-14) was derived from
`requirements.md` v1 only. That gap is now closed — **FAC-21** is the v2 epic, with FAC-22 … FAC-37
covering `requirements-vers-2.md` and `features-phase-2.md` across phases 2a–2f. Two of v2's design
decisions were built rather than filed: **§5.11a entitlement determination** (FAC-15) and the
**§5.5a `RequireAuth` split** (FAC-16).

**FAC-37 is the v2 equivalent of this document's §7 pass** — four v2 §8 questions still gate
estimating (target municipality size, POS build/partner/defer, first-release modules, programme
progression), and two more (cloud region, Canadian data residency) gate all security documentation.

---

**Still open — these need a product decision**

- When does silent residency re-validation fire: once per session, or on entering the booking flow when the cached determination has lapsed? The write-up assumes the latter — revalidate when the price is about to matter — since per-session goes stale on long sessions and per-request is chatty.

- How long does a guest session live? Longer is friendlier for someone returning days later to check a booking; a longer-lived bearer session is also more exposure.

*(Nothing further outstanding — the two financial questions raised above were answered and moved into the answered set.)*
