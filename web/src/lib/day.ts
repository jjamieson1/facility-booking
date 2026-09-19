// Calendar-date arithmetic in YYYY-MM-DD, kept in LOCAL time.
//
// A calendar date is not an instant. `new Date().toISOString().slice(0, 10)`
// formats in UTC, so it answers with a different day than the citizen is
// living in — and it fails in both directions. West of UTC (America/Toronto,
// this municipality's own zone) it rolls over to tomorrow in the evening, so
// the booking form defaulted to tomorrow and its `min` refused today. East of
// UTC, local midnight is the previous day in UTC, so stepping a week landed a
// day early — and with FAC-46's past-time guard, a date that resolves to
// yesterday shows no slots at all.
//
// Everything here formats from LOCAL components and never round-trips through
// UTC. Instants are a separate matter: a booking's start and end are moments
// and are still sent with toISOString(), which is correct for them.

// iso formats a Date as YYYY-MM-DD using its LOCAL calendar fields.
function iso(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

// parse reads YYYY-MM-DD as LOCAL midnight. The "T00:00:00" suffix (with no
// trailing Z) is what makes it local; without it the string is treated as UTC.
function parse(day: string): Date {
  return new Date(`${day}T00:00:00`);
}

/** todayISO is the citizen's own current date, not UTC's. */
export function todayISO(): string {
  return iso(new Date());
}

/** addDaysISO shifts a calendar date by whole days. */
export function addDaysISO(day: string, n: number): string {
  const d = parse(day);
  d.setDate(d.getDate() + n);
  return iso(d);
}

/** addMonthsISO moves to the first of the month n months away. */
export function addMonthsISO(day: string, n: number): string {
  const d = parse(day);
  return iso(new Date(d.getFullYear(), d.getMonth() + n, 1));
}

/** mondayOfISO is the Monday of the week containing day (defaults to today). */
export function mondayOfISO(day: string = todayISO()): string {
  const d = parse(day);
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7)); // Sunday counts as day 7
  return iso(d);
}
