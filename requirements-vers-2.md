  
**Municipal Facility & Recreation**

**Booking Platform**

Product Requirements Document — Version 2.0

Revised against Town of Midland RFP DGSC 2026-043 and City of Edmonton RFEOI 938931

11 August 2026  ·  Supersedes v1.0 (Municipal Facility Booking Requirements)

# **Contents**

[**Contents	2**](#heading=)

[**0\. How to read this revision	3**](#heading=)

[Change markers	3](#heading=)

[Summary of changes	3](#heading=)

[**1\. Executive summary	5**](#heading=)

[Modules	5](#heading=)

[**2\. Goals and scope	5**](#heading=)

[2.1 Goals	5](#heading=)

[2.2 Non-goals	6](#heading=)

[2.3 Platform	6](#heading=)

[**3\. Users and personas	6**](#heading=)

[**4\. Platform requirements	7**](#heading=)

[**5\. Feature requirements	7**](#heading=)

[5.1 Facility and asset directory  EXPANDED	7](#heading=)

[5.2 Availability and scheduling  EXPANDED	8](#heading=)

[5.3 Search by parameters	9](#heading=)

[5.4 Search by date and time	9](#heading=)

[5.5 Booking request and approval  EXPANDED	9](#heading=)

[5.6 Calendar sync and invitations	10](#heading=)

[5.7 Programs and registration  NEW	11](#heading=)

[5.8 Memberships  NEW	11](#heading=)

[5.9 Admissions, retail and point of sale  NEW	12](#heading=)

[5.10 Events, permits and workflow  EXPANDED	13](#heading=)

[5.11 Financial operations  EXPANDED	13](#heading=)

[5.12 Staff back-office  EXPANDED	14](#heading=)

[5.13 My account, cancellation and modification	15](#heading=)

[5.14 Notifications and reminders	15](#heading=)

[5.15 Further capabilities	15](#heading=)

[**6\. Data the platform needs to hold	16**](#heading=)

[**7\. Non-functional requirements	16**](#heading=)

[7.1 Accessibility and usability	16](#heading=)

[7.2 Security and privacy  EXPANDED	16](#heading=)

[7.3 Performance and scale  NEW	17](#heading=)

[7.4 Integration and data portability  NEW	17](#heading=)

[7.5 Service operations  NEW	17](#heading=)

[**8\. Open questions	18**](#heading=)

# **0\. How to read this revision**

Version 1.0 described a facility booking application for a single municipality. Two real procurements have since been analysed, and both are substantially wider in scope. This version incorporates their requirements and reframes the document as a multi-tenant platform rather than a one-off build.

## **Change markers**

Every addition and change carries a highlighted marker and a change bar in the left margin. Anything without a marker is unchanged from version 1.0.

| Marker | Meaning |
| :---- | :---- |
| NEW | Requirement or section that did not exist in v1.0. |
| EXPANDED | Existed in v1.0 but has been widened or deepened. |
| REVISED | Existed in v1.0 but the intent or wording has materially changed. |
| SCALE | Added because of the volumes Edmonton operates at. |

*Sources: Town of Midland RFP \# DGSC 2026-043 (July 2026), and City of Edmonton RFEOI 938931 (September 2025). Neither procurement's detailed requirements matrix was available, so this document is derived from the RFP bodies and municipal practice.*

## **Summary of changes**

| Section | Change | Driver |
| :---- | :---- | :---- |
| 1\. Executive summary | REVISED — reframed from single-municipality app to multi-tenant platform. | Both |
| 2\. Goals and scope | REVISED — mobile and multi-tenancy moved into scope; new non-goals. | Both |
| 3\. Personas | EXPANDED — finance, IT and programme staff added. | Midland |
| 4\. Platform requirements | NEW — multi-tenancy, configuration, modular activation. | Both |
| 5.1 Asset directory | EXPANDED — asset hierarchy and sub-space division. | Edmonton |
| 5.2 Availability | EXPANDED — seasonal rollover and mass update. | Edmonton |
| 5.5 Booking and approval | EXPANDED — concurrency at scale, allocation priority. | Edmonton |
| 5.7 Programs and registration | NEW — 20,000 courses, 100,000 registrations. | Both |
| 5.8 Memberships | NEW — 130,000 members, subsidy categories, access rules. | Edmonton |
| 5.9 Admissions, retail and POS | NEW — counter sales, revenue splitting, inventory. | Both |
| 5.10 Events and permits | EXPANDED — became a full workflow module. | Midland |
| 5.11 Financial operations | EXPANDED — rate blending, GL, invoicing, reconciliation. | Both |
| 6\. Data model | EXPANDED — eleven new entities. | Both |
| 7\. Non-functional | EXPANDED — security, privacy, performance, integration, operations. | Both |
| 8\. Open questions | REVISED — replaced with the decisions this scope now raises. | Both |

# **1\. Executive summary**

 **REVISED**   This document specifies a multi-tenant platform for municipal recreation and facility services, deployable to many municipalities from one codebase. Version 1.0 specified a booking application for a single town. That application is now one module of a larger product.

The core problem is unchanged. Municipalities own valuable spaces and run valuable programmes that are hard for residents to find and book, and hard for staff to administer. Residents do not know what is available, when, or at what cost, and staff spend their time answering the same questions and reconciling a shared calendar by hand.

 **NEW**   What has changed is the understood breadth of that problem. Real procurements show municipalities buying one system to handle facility bookings, programme registration, memberships, drop-in admissions, event permits, retail sales and the financial operations underneath all of it. A product that only books rooms addresses a fraction of the need.

## **Modules**

| Module | Status | Summary |
| :---- | :---- | :---- |
| Platform and tenancy | NEW | Multi-tenant isolation, per-tenant configuration and branding, modular activation. |
| Facility and asset booking | Carried over | The subject of v1.0. Expanded for asset hierarchy and scale. |
| Programs and registration | NEW | Courses, sessions, eligibility, waitlists, rosters, progression. |
| Memberships | NEW | Membership products, categories, subsidies, multi-facility access rules. |
| Admissions, retail and POS | NEW | Counter sales, drop-in admission, revenue splitting, inventory. |
| Events and permits | EXPANDED | Applications, multi-stage approvals, conditions, cost-recovery invoicing. |
| Financial operations | EXPANDED | Fee engine, payments, invoicing, refunds, GL, reconciliation. |
| Administration | Carried over | Configuration, users, audit, staff tooling. |

**Design rule carried into every requirement below: municipalities differ in their rules, not their functions. Rules belong in configuration, never in per-client code.**

# **2\. Goals and scope**

## **2.1 Goals**

* Make it obvious what municipal spaces and programmes exist, what they cost, and when they are available.

* Let residents and groups transact self-serve, at any hour, without contacting the municipality.

* Cut staff time spent answering availability questions and maintaining schedules by hand.

* Prevent double-bookings and keep every calendar accurate.

* Collect fees and required documents up front where a service needs them.

*  **NEW**   Serve many municipalities from one codebase, with local rules expressed as configuration.

*  **NEW**   Allow a municipality to adopt the platform one module at a time, without disrupting live modules.

## **2.2 Non-goals**

* Not a general accounting or ERP system, though it must post to one.

* Not a facilities maintenance or work-order system.

* Not a customer relationship management platform.

*  **REVISED**   Native mobile applications are out of scope for the first release. The public interface is mobile-first responsive web. This is a deliberate deferral rather than a permanent exclusion — see section 8\.

*  **NEW**   Golf and attraction operations (tee sheets, dynamic pricing, rain checks, pro shop) are out of scope for the first release. They apply only to large municipalities.

## **2.3 Platform**

 **REVISED**   A cloud-native, multi-tenant, mobile-first web platform with an open API as a first-class surface. Each municipality receives an isolated tenant with its own configuration, branding and data. There is a public portal for residents and community groups, and a staff back-office for administration and approvals.

# **3\. Users and personas**

| Persona | Status | What they need |
| :---- | :---- | :---- |
| Resident | Carried over | Find a space or programme, see cost and rules, book and pay quickly. |
| Community group organiser | Carried over | Recurring bookings, group pricing, easy re-booking, clear policies. |
| Facility staff / administrator | Carried over | Manage assets and availability, approve requests, avoid conflicts. |
| Clerk / finance | EXPANDED | Fee configuration, reconciliation, GL posting, refunds, revenue reporting. |
| Programme co-ordinator | NEW | Build seasons and courses, manage rosters, waitlists and instructors. |
| Front counter staff | NEW | Take walk-in payments and admissions quickly, balance a till at day end. |
| Municipal IT | NEW | Security review, SSO integration, data residency, API access, audit. |

# **4\. Platform requirements**

 **NEW**   This entire section is new. Version 1.0 assumed a single deployment for a single municipality and therefore had no need for it.

As a product company, we need one codebase to serve many municipalities without forking, and each municipality needs to feel that the system is theirs.

### **Details**

*  **NEW**   Each municipality is an isolated tenant. Data, configuration and branding never cross tenants.

*  **NEW**   Per-tenant branding: logo, colours, and public portal domain.

*  **NEW**   Per-tenant terminology so local naming is used — a slip, a berth, a diamond, a pad.

*  **NEW**   Modules can be licensed and enabled independently, so a municipality may take booking only and add registration later.

*  **NEW**   Separate production, staging and training environments per tenant, with configuration promoted between them without a code deployment.

*  **NEW**   Configuration changes are versioned and attributable, and can be rolled back.

### **Acceptance criteria**

Given two municipalities are configured with different fee rules

When each books the same class of asset

Then each is charged according to its own rules with no code difference between them

Given a municipality has licensed only the booking module

When a resident visits the public portal

Then only booking functions are visible, and registration is not offered

Given a configuration change is made in staging

When it is promoted to production

Then it takes effect without a software release

# **5\. Feature requirements**

Each feature states the user's need and the observable outcome, followed by acceptance criteria a tester can check pass or fail. Implementation choices are left to the developer unless a specific behaviour is required.

## **5.1 Facility and asset directory  EXPANDED**  

As a resident, I want to browse the municipality's bookable assets and see everything I need to decide, so I can choose the right space without calling.

### **Details**

* Each asset shows name, size or capacity, cost, location, the building it belongs to, and connected assets such as an attached kitchen.

* Amenities and equipment available in the space are listed, with quantities where relevant.

* Before-use and after-use instructions are shown, covering setup, access, cleanup and lockup.

* Photos, floor plans where available, and accessibility details including step-free access, accessible washrooms, parking and hearing loop.

*  **EXPANDED**   Assets form a hierarchy — site, building, space, sub-space — and a parent may be bookable in its own right or only through its children.

*  **NEW**   A single space can be divided into independently bookable stations. A pool becomes lanes, a gymnasium becomes courts, a hall becomes halves. Booking one station must correctly constrain the availability of the parent and its siblings.

*  **SCALE**   The directory must remain usable and performant at 8,000 or more assets across 20 or more facilities.

### **Acceptance criteria**

Given I open an asset's detail page

Then I see its capacity, cost, location, connected assets, amenities and before/after instructions

Given a gymnasium is divided into three courts

When court 2 is booked

Then courts 1 and 3 remain available and the whole-gymnasium option becomes unavailable

Given an asset is free of charge

Then the cost is shown explicitly as free rather than left blank

## **5.2 Availability and scheduling  EXPANDED**  

As a resident, I want to see when an asset is free so I can pick a time that works; and as staff, I want to maintain those schedules without editing thousands of records by hand.

### **Details**

* Each asset has a calendar showing booked, open and unavailable (blackout or maintenance) periods.

* Availability reflects the asset's booking rules — opening hours, minimum and maximum duration, and buffer time between bookings for setup and cleanup.

*  **NEW**   Seasonal rollover: staff can roll an entire season's schedules, rules and rates forward to the next season in one operation, with a preview of what will change before it is applied.

*  **NEW**   Mass update tooling can apply a change across a selected set of assets at once, with the ability to review and reverse it.

### **Acceptance criteria**

Given an asset has bookings and open slots

Then booked times are visibly distinct from open times

Given a new season is being prepared across 500 assets

When staff run a seasonal rollover

Then the schedules and rates are created for the new season and a preview is shown before commit

Given a mass update has been applied in error

When staff reverse it

Then affected assets return to their previous state

## **5.3 Search by parameters**

As a resident, I want to set my requirements and see only assets that match, so I do not scroll through spaces that will not work.

* Filterable parameters include capacity, required amenities, cost range or free-only, location or area, and accessibility needs.

* Filters combine, and a clear empty state appears when nothing matches, suggesting which filter to relax.

## **5.4 Search by date and time**

As a resident, I want to pick a date and time and see what is free then, so I can plan around my own schedule.

* The user selects a date and optionally a time range; the platform returns assets open for that window.

* Date search combines with the parameter filters in 5.3.

## **5.5 Booking request and approval  EXPANDED**  

As a resident, I want to request a booking; and as staff, I want to review requests so the municipality keeps control of its assets.

### **Details**

* A booking captures the asset, date and time, purpose, expected attendance, and the requester's contact details.

* Assets can be set to auto-confirm or to require staff approval, and approval may carry conditions, fees or required documents.

* Both requester and staff are notified at each step: submitted, approved, denied, cancelled.

*  **EXPANDED**   Double-booking prevention must hold under genuine concurrency, not merely sequential requests. A confirmed booking blocks the slot for all other in-flight requests, including across parent and child assets.

*  **NEW**   Allocation priority windows: affiliated or resident groups may be granted exclusive access to book for a defined period before general release.

*  **NEW**   Recurring and seasonal bookings can be requested as a single application, with conflicts across the series reported before submission.

### **Acceptance criteria**

Given an asset requires approval

When I submit a request

Then it is held as pending and staff are notified

Given two requests for the same slot arrive simultaneously

When the first is confirmed

Then the second is rejected with a clear message and no double-booking is created

Given a priority window is open for affiliated groups

When a member of the public attempts to book

Then they are told when general booking opens

## **5.6 Calendar sync and invitations**

As staff, I want confirmed bookings on the municipal calendar; as a booker, I want the reservation in my own calendar.

* Confirmed bookings sync to the municipality's calendar platform, and the booker receives a calendar invitation.

* Cancellations and changes update or withdraw the corresponding calendar entries.

## **5.7 Programs and registration  NEW**  

 **NEW**   This module did not exist in v1.0. Both procurements treat programme registration as core, and Edmonton runs roughly 20,000 courses and over 100,000 registrations a year.

As a programme co-ordinator, I want to publish courses and manage who is enrolled, so residents can register themselves and staff can run the season.

### **Details**

*  **NEW**   Programmes, sessions and classes are defined with seasons, schedules and repeating patterns, built from reusable templates rather than entered one at a time.

*  **NEW**   Registration opens and closes on configurable dates, with optional resident-priority windows.

*  **NEW**   Eligibility rules by age, date of birth, skill level or completed prerequisite.

*  **NEW**   Progression tracking through skill levels, with advancement recorded on completion and used to satisfy the prerequisite for the next level.

*  **NEW**   Capacity limits with automatic waitlists, and automatic promotion from the waitlist when a place is released.

*  **NEW**   Participant records including emergency contacts and medical or accessibility notes, visible only to authorised staff.

*  **NEW**   Instructor assignment, attendance tracking and class rosters usable on a mobile device.

*  **NEW**   Withdrawals and transfers between sessions, with policy-driven refunds or account credits.

*  **SCALE**   Registration opening is the highest-load event in the system. It must handle a surge of 100 or more registrations per second without oversell, lost registrations or queue failure.

### **Acceptance criteria**

Given a course requires completion of the preceding level

When a resident without that level attempts to register

Then registration is refused with an explanation of the prerequisite

Given a course is full and has a waitlist

When a registered participant withdraws

Then the first person on the waitlist is offered the place automatically

Given 500 people attempt to register in the first seconds of opening

When capacity is 100

Then exactly 100 registrations succeed, the remainder are waitlisted, and none are lost

## **5.8 Memberships  NEW**  

 **NEW**   This module did not exist in v1.0. Edmonton manages more than 130,000 members across 20 or more facilities.

As a resident, I want a membership that admits me to the facilities I use; and as staff, I want membership rules enforced automatically at every point of entry.

### **Details**

*  **NEW**   Membership products with configurable term, price, entitlements and the facilities and times they admit.

*  **NEW**   Categories including individual, family, corporate and subsidised, each with its own pricing and eligibility.

*  **NEW**   Fee-assistance and low-income subsidy programmes, with eligibility verification and discreet handling at the point of sale.

*  **NEW**   One membership valid across multiple facilities, with access rules evaluated per facility and per time of day.

*  **NEW**   Lifecycle handling: join, renew, freeze, transfer and cancel, with pro-rating where applicable.

*  **NEW**   Entitlement checks applied automatically when a member books a space or registers for a programme.

### **Acceptance criteria**

Given a membership admits only to off-peak hours

When the member attempts entry during peak hours

Then entry is refused and the applicable drop-in fee is offered

Given a family membership covers four named dependants

When a fifth person attempts to use it

Then they are not admitted under that membership

Given a member is enrolled in a subsidy programme

When they transact at a counter

Then the subsidised rate applies without disclosing their status to bystanders

## **5.9 Admissions, retail and point of sale  NEW**  

 **NEW**   This module did not exist in v1.0. Both procurements require counter transactions; v1.0 assumed all payment happened on a web form.

As front counter staff, I want to sell admissions and goods quickly and balance my till at the end of the day, so the front desk keeps moving and the money reconciles.

### **Details**

*  **NEW**   Point of sale for counter transactions, supporting payment terminal hardware, with a defined list of supported devices.

*  **NEW**   Drop-in admission with fast credential validation, suitable for a queue at a busy facility.

*  **NEW**   Revenue splitting across departments or partners within a single transaction.

*  **NEW**   Merchandise inventory with stock levels, receiving and adjustments, and retail sale through the same till.

*  **NEW**   Cash handling: float management, till reconciliation and daily cash-out reporting.

*  **NEW**   Transactions at the counter attach to the same customer account as online activity, so history is unified.

### **Acceptance criteria**

Given a member presents at the front desk

When their credential is validated

Then admission is granted or refused within two seconds

Given a single sale includes an admission and a merchandise item with different revenue accounts

When the sale completes

Then revenue is split to the correct accounts automatically

Given a shift has ended

When staff run cash-out

Then expected and counted amounts are compared and any variance is recorded

## **5.10 Events, permits and workflow  EXPANDED**  

As an applicant, I want to apply for an event permit and see where my application stands; as staff, I want applications routed to the right people and decided on the record.

### **Details**

*  **EXPANDED**   Application forms are configurable per permit or event type, built by staff without code.

*  **EXPANDED**   Approval workflow supports multiple stages, routed by department, application type or value, including parallel approvals where several departments must sign off independently.

* Document upload for insurance certificates, site plans and signed waivers, with expiry tracking and renewal reminders.

* Applicant-visible status tracking, with internal staff notes kept separate from applicant correspondence.

*  **NEW**   Conditions can be attached to an approval, which the applicant must acknowledge before the permit issues.

*  **NEW**   Post-event invoicing for actual costs incurred, reconciled against the permit and the original estimate.

*  **NEW**   Configurable service standards, with escalation when an application stalls beyond its target.

### **Acceptance criteria**

Given an application requires approval from three departments

When two have approved and one has not

Then the permit does not issue and the outstanding department is identified

Given an approval carries conditions

When the applicant has not acknowledged them

Then the permit remains unissued

Given an event has taken place and incurred costs above the estimate

When staff finalise the permit

Then an invoice for actual costs is generated and reconciled against the permit

## **5.11 Financial operations  EXPANDED**  

 **EXPANDED**   Version 1.0 covered fees and online payment. Both procurements require full financial operations, and Edmonton processes roughly $70M of transactions a year.

As finance staff, I want fees configured once and applied consistently, and every transaction to reconcile to the general ledger without manual re-keying.

### **Details**

* Paid services show the fee and any deposit before the user commits, and payment is taken at booking or on approval.

* Refunds follow the cancellation policy; deposits can be held and released.

* Resident and non-resident pricing is supported where the municipality uses it.

*  **EXPANDED**   The fee engine sets rates by asset, time of day, day of week, season, duration and customer category, including non-profit and commercial rates.

*  **NEW**   Rate blending: a single booking spanning high, medium and low demand windows is charged the correct rate for each portion, not a single flat rate.

*  **NEW**   Tax configuration per fee type, with correct handling of exemptions.

*  **NEW**   Invoicing for accounts billed after the fact, with payment terms and aging.

*  **NEW**   General ledger coding per fee, and an export or interface to the municipality's financial system.

*  **NEW**   Daily reconciliation and revenue reporting, with export to CSV and Excel, and an ad-hoc report builder staff can use without vendor involvement.

*  **REVISED**   No patron-facing convenience or transaction surcharge is charged in the base commercial model.

### **Acceptance criteria**

Given a booking runs from 4pm to 8pm and the rate changes at 6pm

When the fee is calculated

Then the first two hours are charged at the earlier rate and the last two at the later rate

Given fees are coded to general ledger accounts

When a day's transactions are exported

Then each line carries its GL code and the export balances to the day's takings

Given I cancel within the refundable window

Then a refund is issued per the policy and the reversal is reflected in the GL export

## **5.12 Staff back-office  EXPANDED**  

As staff, I want to manage assets, availability, programmes and requests in one place, so the public site stays accurate.

* Create and edit assets and all their details; set availability, opening hours, blackout dates and buffer times.

* Review, approve, deny or add conditions to requests; view the schedule across all assets.

* Roles and permissions so only authorised staff can approve or edit.

*  **EXPANDED**   Permissions are scoped by department so staff see only their own service area.

*  **NEW**   Bulk import and edit tooling for assets, customers, schedules and programmes.

*  **NEW**   A staff dashboard summarising pending approvals, today's bookings and exceptions needing attention.

*  **NEW**   A training environment with representative data, so staff can be trained without touching live records.

## **5.13 My account, cancellation and modification**

As a customer, I want to see and manage everything I have with the municipality in one place.

*  **EXPANDED**   The account shows bookings, programme registrations, memberships, permits and payment history together, whether transacted online or at a counter.

* Self-service cancellation and modification within configurable policy windows, with policy applied automatically.

## **5.14 Notifications and reminders**

As a user, I want to be told when something needs me, so I do not miss an obligation or a booking.

* Confirmations, approvals, denials and reminders by email, with SMS as a configurable option.

* Reminders include access instructions and before-use requirements.

*  **NEW**   Notification templates are configurable per tenant, with municipal wording and branding.

## **5.15 Further capabilities**

* Map view of assets, and directions from an asset's address.

* Waitlists on popular assets, with notification when a slot frees.

* Public read-only availability view accessible without an account.

* Utilisation, revenue and demand reporting.

*  **NEW**   Bilingual English and French interface, selectable per tenant.

*  **NEW**   Website embedding, so availability and registration can appear on the municipality's own site.

# **6\. Data the platform needs to hold**

Described at the level of what is stored, not how. The developer chooses the schema.

| Entity | Status | Key information |
| :---- | :---- | :---- |
| Tenant | NEW | The municipality: configuration, branding, terminology, enabled modules. |
| Asset | EXPANDED | Hierarchy and parent, capacity, attributes, amenities, instructions, media, accessibility, booking rules. |
| Availability | Carried over | Opening hours, bookable periods, blackouts, buffers. |
| Booking | Carried over | Asset, date and time, customer, purpose, attendance, status, fee. |
| Customer | EXPANDED | Account, household members, residency status, contact details, unified history. |
| Programme | NEW | Course, session, schedule, capacity, eligibility, prerequisites, instructor. |
| Registration | NEW | Participant, programme, status, attendance, progression, waitlist position. |
| Membership | NEW | Product, category, term, entitlements, access rules, subsidy status. |
| Permit application | NEW | Form data, documents, approval stages, conditions, decisions, costs. |
| Fee schedule | NEW | Rate rules by asset, time, season, category; tax treatment; GL codes. |
| Transaction | EXPANDED | Payments, refunds, deposits, splits, channel (online or counter), GL posting. |
| Inventory item | NEW | Merchandise, stock level, receiving and adjustment history. |
| User and role | EXPANDED | Staff accounts, roles, department scope, permissions. |
| Audit record | NEW | Who changed what, when, across bookings, permits, refunds and configuration. |
| Integration record | NEW | External identifiers linking bookings, calendars and financial postings. |

# **7\. Non-functional requirements**

## **7.1 Accessibility and usability**

* Meets WCAG 2.1 AA across public and staff interfaces.

*  **EXPANDED**   Alignment with AODA in Ontario and the equivalent provincial obligations elsewhere, with a published accessibility conformance report available on request.

*  **REVISED**   Mobile-first design for all public-facing functions. Staff functions must remain usable on a tablet.

* Bilingual capability where the municipality requires it.

## **7.2 Security and privacy  EXPANDED**  

*  **NEW**   All customer data stored and processed within Canada.

*  **NEW**   Named sub-processor list, with advance notice of any change.

*  **NEW**   Compliance with the applicable municipal privacy regime — MFIPPA in Ontario, FOIP in Alberta — including support for access and correction requests.

*  **NEW**   PCI-DSS compliant payment handling, with no card data stored in the platform.

*  **NEW**   Single sign-on for staff via the municipality's identity provider, and multi-factor authentication on all administrative accounts.

*  **NEW**   Encryption in transit and at rest; annual third-party penetration testing with a summary available to clients.

*  **NEW**   Immutable audit log of staff actions on bookings, permits, refunds and configuration.

*  **NEW**   Security incident detection and notification to the municipality within a defined period.

*  **NEW**   Documented backup, retention and disaster recovery with stated recovery point and recovery time objectives.

## **7.3 Performance and scale  NEW**  

 **SCALE**   Entirely new. Version 1.0 stated only that common actions should feel fast. Edmonton publishes hard numbers, and they are architecture constraints rather than aspirations.

*  **SCALE**   Sustain 10,000 concurrent users with no measurable service degradation.

*  **SCALE**   Sustain peak registration load of 100 or more transactions per second.

*  **SCALE**   Support 8,000 or more bookable assets and 130,000 or more member records without degradation of search or listing.

*  **NEW**   Cloud-native architecture that scales horizontally on demand, with headroom for annual transaction growth without re-platforming.

*  **NEW**   Load and soak test evidence available to prospective clients.

*A small municipality will never approach these numbers. They are stated so the architecture does not foreclose a large client later. The expensive optimisations should be deferred until such a client is genuinely in view.*

## **7.4 Integration and data portability  NEW**  

*  **NEW**   Documented, versioned public REST API covering all core entities, treated as a product surface rather than an afterthought.

*  **NEW**   Data migration tooling to import customers, assets, bookings and history from a legacy system, with validation and reconciliation reporting before cutover.

*  **NEW**   Full customer data export in a non-proprietary format, available on demand.

*  **NEW**   Transition assistance on exit at rates published in the agreement. No lock-in.

*  **NEW**   Financial system integration for GL postings and receivables; calendar integration with Microsoft 365 and Google Workspace.

## **7.5 Service operations  NEW**  

*  **NEW**   Service level agreement with a stated uptime target and how it is measured.

*  **NEW**   Support model with defined severity levels and response and resolution targets.

*  **NEW**   Published maintenance windows, version and patch register per component, and a stated support lifecycle.

*  **NEW**   Published product roadmap and release notes.

*  **NEW**   Implementation methodology supporting phased delivery by service area, with partial deployments operable alongside existing systems.

# **8\. Open questions**

 **REVISED**   The v1.0 questions have been answered by the two procurements or overtaken by the change in scope. These are the decisions this scope now raises.

1. Which municipality size is the design target? Requirements from a town of 20,000 and a city of 1,000,000 differ by two orders of magnitude, and building for both at once is how products lose focus.

2. Point of sale: build, partner or defer? It is mandatory for recreation buyers and it is hardware-dependent, so it cannot be decided late.

3. Which modules constitute the first release? Booking plus financial operations is the smallest combination that is genuinely sellable.

4. Is native mobile deferred permanently or scheduled? Both procurements accept mobile-first responsive web, so deferral looks safe, but it should be a decision rather than an omission.

5. Which payment processor and which cloud region? Both are needed before any security documentation can be written.

6. Do we commit to Canadian data residency in all cases? For municipal clients this is effectively mandatory, and it constrains the hosting choice.

7. How is programme progression modelled — municipality-defined level schemes, or a fixed model? Swimming levels differ between municipalities and this is a configuration question with real depth.

8. Is golf and attraction management ever in scope, or permanently declined? Only large municipalities need it, and it is a substantial module.

*End of document.*