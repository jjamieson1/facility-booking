# facility-booking (FAC)

An application to allow an organization to manage their facility bookings

These are the documents captured when this project was created. Read them before planning or implementing: they describe how this project is built and what it must comply with, and they take precedence over general defaults where the two differ.

## Architecture

- `architecture.md` — React/Vite/Typescript/Golang

## Deployment

No deployment document is set for this project.

## Compliance

No compliance standards are recorded for this project.

## App Builder

This project builds on the following DevPro App Builder services. Integrate against them rather than reimplementing what they provide.

- `builder/authentication-profile.md` — **Authentication and profile**: Sign users in through C2 (OIDC + PKCE) and read their profile, instead of building your own identity.
- `builder/notifications.md` — **Notification Services**: Send notifications to a citizen through C2, which handles the consent gate and delivery channels.
- `builder/payments.md` — **Payment Service**: C2 brokers the payment. No integration guide published yet — confirm scope with the C2 team first.
- `builder/application-status.md` — **Application Status**: Answer C2's service card callout so a citizen sees your application's status in their portal.
- `builder/security-service-scanning.md` — **Security Pipeline**
