  
**Municipal Facility Booking**

Product Requirements Document

Web application for small cities and municipalities

Version 1.0  ·  July 2026

# **Contents**

[**Contents	2**](#heading=)

[**1\. Executive summary	3**](#heading=)

[Core features (v1)	3](#heading=)

[Additional features worth including	3](#heading=)

[**2\. Product overview and goals	4**](#heading=)

[2.1 Goals	4](#heading=)

[2.2 Non-goals (v1)	4](#heading=)

[2.3 Platform	4](#heading=)

[**3\. Users and personas	5**](#heading=)

[**4\. Feature requirements	6**](#heading=)

[4.1 Facility directory and detail	6](#heading=)

[4.2 Availability calendar	6](#heading=)

[4.3 Search by parameters	7](#heading=)

[4.4 Search by date and time	7](#heading=)

[4.5 Booking request and approval	7](#heading=)

[4.6 Calendar sync and invites	8](#heading=)

[4.7 Fees and payment	8](#heading=)

[4.8 Staff back-office	9](#heading=)

[4.9 My bookings, cancellation and modification	9](#heading=)

[4.10 Notifications and reminders	9](#heading=)

[4.11 Further useful features	10](#heading=)

[**5\. Data the app needs to hold	11**](#heading=)

[**6\. Non-functional requirements	12**](#heading=)

[**7\. Open questions for the developer	13**](#heading=)

# **1\. Executive summary**

Municipal Facility Booking is a web app that lets residents and community groups find and book publicly available spaces — meeting rooms, halls, sports fields, park pavilions — and lets municipal staff manage those bookings from one place. It replaces phone calls, paper forms, and back-and-forth email with a self-serve site backed by a staff approval and calendar workflow.

The core problem: small municipalities own valuable spaces that are hard to book. Residents don't know what's available, when, or at what cost, and staff spend time answering the same questions and manually tracking a shared calendar. This app makes availability visible, lets people filter to a space that fits their needs and date, and keeps the city's calendar and the booker's calendar in sync automatically.

## **Core features (v1)**

* **Facility directory —** browse bookable facilities, each showing size/capacity, cost (if any), location, connected facilities, available accessories (projector, coffee maker, chairs, tables), and before/after instructions.

* **Availability calendar —** see each facility's open and booked dates and times.

* **Filter by parameters —** set requirements (capacity, accessories, cost, location) and see only facilities that match.

* **Filter by date —** pick a date/time and see which facilities are free then.

* **Calendar sync —** bookings sync to the city's calendar, and the booker gets a calendar invite for their reservation.

* **Booking request —** submit a request for a facility and time, with staff approval where required.

## **Additional features worth including**

Beyond the requested list, the following add clear value for a small municipality and are detailed later: a staff back-office to manage facilities, availability, and approvals; online fee payment for paid spaces; resident vs non-resident pricing; recurring bookings for regular community groups; double-booking prevention; cancellation and modification with policy; booking history and "my bookings"; email/SMS confirmations and reminders; accessibility details per facility; photos and floor plans; a map view; waivers/insurance upload; a waitlist for popular spaces; and utilization/revenue reporting for the municipality.

*The rest of this document expands each feature into detailed requirements with acceptance criteria, notes the data the app needs to hold, and lists non-functional requirements and open questions for the developer.*

# **2\. Product overview and goals**

## **2.1 Goals**

* Make it obvious what municipal spaces exist, what they cost, and when they're free.

* Let residents and groups book self-serve, at any hour, without phoning the city.

* Cut staff time spent answering availability questions and manually tracking a calendar.

* Prevent double-bookings and keep the city calendar and bookers' calendars accurate.

* Collect fees and required documents up front where a facility needs them.

## **2.2 Non-goals (v1)**

* Not a full financial/ERP system — it collects fees but isn't the city's accounting system.

* Not a facilities-maintenance or work-order system.

* Not an events-ticketing or public-event-promotion platform.

* No native mobile app in v1; the web app must work well in a mobile browser.

## **2.3 Platform**

Responsive web application. A public-facing side for residents and groups to browse and book, and a staff/administrator side to manage facilities, availability, approvals, and reporting. Cloud-hosted so bookings and the calendar are shared and current across everyone.

# **3\. Users and personas**

| Persona | Who they are | What they need |
| :---- | :---- | :---- |
| Resident | A community member booking a room or space for personal or family use. | Find a suitable space, see cost and rules, book and pay quickly, get it on their calendar. |
| Community group organiser | Runs a club, league, or non-profit that books regularly. | Recurring bookings, group/resident pricing, easy re-booking, clear policies. |
| Facility staff / administrator | Municipal employee who manages spaces and approves requests. | Manage facilities and availability, approve/deny requests, prevent conflicts, see the schedule. |
| Clerk / finance | Handles fees, refunds, and reporting. | Track payments and refunds, run utilization and revenue reports. |

# **4\. Feature requirements**

Each feature is written as the user's need and the observable outcome, followed by acceptance criteria a tester can check pass/fail. Implementation choices (screens, data shapes, integrations) are left to the developer unless a specific behaviour is required.

## **4.1 Facility directory and detail**

As a resident, I want to browse the municipality's bookable facilities and see everything I need to decide, so I can choose the right space without calling the city.

### **Details**

* Each facility shows: name, size/capacity, cost (or free), location/address, the parent facility or building it belongs to, and connected facilities (e.g. a hall with an attached kitchen).

* Accessories available in the space are listed (projector, screen, coffee maker, chairs, tables, sound system, Wi-Fi), with quantities where relevant.

* Before-use and after-use instructions are shown (setup, keys/access, cleanup, waste, lockup).

* Photos and, where available, a floor plan; accessibility details (step-free access, accessible washroom, parking, hearing loop).

### **Acceptance criteria**

Given I open a facility's detail page

When it loads

Then I see its capacity, cost, location, connected facilities, accessories, and before/after instructions

Given a facility is free of charge

When I view it

Then the cost is clearly shown as free rather than left blank

## **4.2 Availability calendar**

As a resident, I want to see when a facility is free so I can pick a time that works.

### **Details**

* Each facility has a calendar showing booked, open, and unavailable (blackout/maintenance) slots.

* Availability reflects the facility's booking rules — opening hours, minimum/maximum duration, and buffer time between bookings for setup/cleanup.

### **Acceptance criteria**

Given a facility has bookings and open slots

When I view its calendar

Then booked times are visibly distinct from open times

Given a facility is closed for maintenance on a date

When I view that date

Then it shows as unavailable and cannot be booked

## **4.3 Search by parameters**

As a resident, I want to set my requirements and see only facilities that match, so I don't scroll through spaces that won't work.

### **Details**

* Filterable parameters include capacity (minimum people), required accessories, cost range or free-only, location/area, and accessibility needs.

* Filters combine (all must match) and results update to show matching facilities.

* A clear empty state appears when nothing matches, suggesting which filter to relax.

### **Acceptance criteria**

Given I set capacity \>= 50 and require a projector

When I apply the filters

Then only facilities that seat 50+ and have a projector are shown

Given no facility matches my filters

When results load

Then I see an empty state suggesting I adjust or remove a filter

## **4.4 Search by date and time**

As a resident, I want to pick a date and time and see which facilities are free then, so I can book around my own schedule.

### **Details**

* The user selects a date and (optionally) a time range; the app returns facilities open for that window.

* Date search combines with the parameter filters in 4.3.

### **Acceptance criteria**

Given I choose Saturday 2-5 PM

When I search

Then only facilities free for that whole window are shown

Given I add a capacity filter to a date search

When I search

Then results are free at that time AND meet the capacity filter

## **4.5 Booking request and approval**

As a resident, I want to request a booking for a facility and time, so I can reserve the space; and as staff, I want to review requests so the city keeps control of its facilities.

### **Details**

* A booking captures the facility, date/time, purpose, expected attendance, and the booker's contact details.

* Facilities can be set to auto-confirm or require staff approval; approval can include conditions, fees, or required documents.

* The system prevents double-booking — a confirmed booking blocks that slot for everyone, including concurrent requests.

* Booker and staff are notified at each step (submitted, approved, denied, cancelled).

### **Acceptance criteria**

Given a facility requires approval

When I submit a booking request

Then it is held as pending and staff are notified to review it

Given two people request the same slot

When the first is confirmed

Then the second can no longer book that slot and is told it's taken

Given staff approve my request

Then I receive a confirmation and the booking appears as confirmed

## **4.6 Calendar sync and invites**

As staff, I want confirmed bookings to appear on the city calendar; and as a booker, I want the reservation added to my own calendar, so nothing is tracked by hand.

### **Details**

* Confirmed bookings sync to the municipality's calendar (e.g. Google/Microsoft/Outlook or via iCal feed).

* The booker receives a calendar invite (.ics) for their reservation; cancellations and changes update or withdraw it.

* Sync is two-way enough that events blocked directly on the city calendar show as unavailable in the app (or this is called out as a limitation — see open questions).

### **Acceptance criteria**

Given my booking is confirmed

When confirmation is sent

Then I receive a calendar invite for the correct facility, date, and time

Given a confirmed booking is cancelled

When the cancellation is processed

Then the city calendar entry and the booker's invite are removed or updated

## **4.7 Fees and payment**

As a resident booking a paid space, I want to pay online, so my booking is confirmed without a separate trip or call.

### **Details**

* Paid facilities show the fee (and any deposit) before the user commits; the app collects payment at booking or on approval.

* Refunds follow the cancellation policy; deposits can be held and released.

* Resident vs non-resident pricing is supported where the municipality uses it.

### **Acceptance criteria**

Given a facility has a $75 fee

When I book it

Then I'm shown the fee and asked to pay before the booking is confirmed

Given I cancel within the refundable window

When the cancellation is processed

Then a refund is issued per the policy

## **4.8 Staff back-office**

As facility staff, I want to manage facilities, availability, and requests in one place, so the public site stays accurate.

### **Details**

* Create and edit facilities and all their details (capacity, cost, accessories, instructions, photos, rules).

* Set availability, opening hours, blackout dates, and buffer times; block a facility for maintenance.

* Review, approve, deny, or add conditions to booking requests; view the full schedule across facilities.

* Roles/permissions so only authorised staff can approve or edit.

### **Acceptance criteria**

Given I am authorised staff

When I mark a facility unavailable for a date range

Then residents cannot book it in that range and it shows as unavailable

Given a pending request

When I approve it

Then the booker is notified and the slot is confirmed and synced

## **4.9 My bookings, cancellation and modification**

As a booker, I want to see and manage my bookings, so I can change or cancel without contacting the city.

### **Acceptance criteria**

Given I have upcoming bookings

When I open my bookings

Then I see each booking's facility, date/time, status, and cost

Given a booking is within the modifiable window

When I cancel or request a change

Then the policy is applied and staff and the calendar are updated

## **4.10 Notifications and reminders**

As a booker, I want confirmations and reminders so I don't forget or miss my slot; as staff, I want to be alerted to requests needing action.

### **Acceptance criteria**

Given my booking is confirmed

When the date approaches

Then I receive a reminder with the before-use instructions and access details

Given a request needs approval

When it is submitted

Then the responsible staff are notified

## **4.11 Further useful features**

* **Recurring bookings —** let a regular group book a repeating slot (e.g. every Tuesday) with one request, respecting conflicts.

* **Map view —** show facilities on a map so residents can find spaces near them.

* **Waivers and insurance upload —** require a signed waiver or proof of insurance for certain facilities before confirmation.

* **Waitlist —** let residents join a waitlist for a booked slot and be notified if it frees up.

* **Reporting —** utilization, revenue, and demand reports so the municipality can plan and justify spending.

* **Multilingual —** support the municipality's official languages (in Canada, English and French).

* **Resident verification —** confirm residency (address check) where resident pricing or priority applies.

* **Public availability view —** a read-only calendar the public can see without an account, to reduce enquiries.

# **5\. Data the app needs to hold**

Described at the level of what's stored, not how. The developer chooses the schema.

| Entity | Key information |
| :---- | :---- |
| Facility | Name, capacity/size, cost/pricing, location, parent building, connected facilities, accessories, before/after instructions, photos, floor plan, accessibility, booking rules. |
| Availability | Opening hours, bookable slots, blackout/maintenance dates, buffer times per facility. |
| Booking | Facility, date/time, booker, purpose, attendance, status (pending/confirmed/denied/cancelled), fee, documents. |
| User | Resident or group account: name, contact, residency status, role (public/staff/admin). |
| Payment | Amount, deposit, status, refunds, tied to a booking. |
| Calendar link | References/ids connecting a booking to the city calendar event and the booker's invite. |
| Accessory | Name, quantity, which facilities offer it. |
| Policy | Cancellation/refund rules, waiver/insurance requirements per facility. |

# **6\. Non-functional requirements**

* **Accessible —** public sector: meets WCAG 2.1 AA (and be mindful of AODA in Ontario / provincial equivalents). Keyboard, contrast, screen-reader labels, alt text.

* **Responsive —** fully usable on phones and desktops; touch-friendly.

* **Reliable —** no double-bookings under concurrent requests; availability is always accurate.

* **Secure —** protects personal data and payments; PCI-compliant payment handling (via a provider, not storing card data).

* **Privacy —** handles residents' personal information per applicable privacy law (in Canada, PIPEDA / provincial FOIP/MFIPPA).

* **Auditable —** staff actions on bookings (approve, deny, refund) are logged for accountability.

* **Available —** self-serve booking works outside office hours; sensible uptime for a public service.

# **7\. Open questions for the developer**

Decisions that shape scope and effort, worth settling before build:

* Which calendar platform does the municipality use (Google Workspace, Microsoft 365/Outlook, other), and is two-way sync required or is a one-way push plus .ics invite enough for v1?

* Do residents need accounts, or can they book as guests with email verification? How is residency verified for resident pricing?

* Which payment provider, and does the city need refunds/deposits handled in-app or through an existing system?

* How is staff approval structured — one approver, per-facility approvers, or by department?

* Are both official languages required at launch, and does existing city branding/design need to be matched?

* Does this need to integrate with any existing municipal system (e.g. a permit or finance system, or single sign-on for staff)?

* What are the facility booking rules today (min/max duration, buffer, cancellation windows) that the app must enforce?

*End of document.*