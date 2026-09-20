# Where these came from

Pulled from the Celestial-Ticket project record with `get_project_docs` (FAC).
They are the documents captured when this project was created, plus an
integration guide for each DevPro App Builder (C2) platform service this app
builds on.

They are **authoritative for how this app integrates with C2** and take
precedence over our own defaults where the two differ. `builder/payments.md` and
`builder/notifications.md` are the on-the-wire contracts for the partner API;
`builder/application-status.md` is the service-card callout we answer.

Refresh them with `get_project_docs` rather than editing by hand — they are
owned upstream, and a local edit would be overwritten and lost.

Read alongside `CLAUDE.md`, which records what *this* codebase does and why.
Where CLAUDE.md and a guide disagree, the guide describes the platform and
CLAUDE.md describes us — which usually means we have drifted.
