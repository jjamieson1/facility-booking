// The brand identity, in one place (FAC-20).
//
// This is the *text and mark* half of the brand; the palette lives beside it as
// CSS custom properties in index.css. Both are deliberately single-source so
// deploying for another municipality is a config change rather than a hunt
// through components.
//
// The .ics feed carries its own copy of the name, because it is produced by the
// Go API which cannot read this file — see FB_BRAND_NAME in the API config. The
// two must be set together.
export const brand = {
  // The service name, shown in the header and the browser tab. Not translated:
  // a municipality's service name is a proper noun, and translating it would
  // invent a second identity that appears on no letterhead.
  name: "Rivermont Spaces",
  // The mark in the header tile. One or two characters — it is rendered as
  // text, so it needs no asset and cannot 404. A municipality with a real logo
  // replaces the tile in App.tsx with an <img>, which is why the tile is one
  // component and not repeated.
  mark: "R",
  // Appended to the service name in the browser tab, so a tab in a crowded
  // window still says what the page is for.
  tagline: "Book a municipal facility",
} as const;

// documentTitle is what the browser tab shows.
export function documentTitle(): string {
  return `${brand.name} — ${brand.tagline}`;
}
