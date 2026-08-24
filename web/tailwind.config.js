/** @type {import('tailwindcss').Config} */

// The brand palette resolves through CSS custom properties rather than being
// baked into the build. Rebranding for another municipality is then an edit to
// one `:root` block in index.css — or a stylesheet served per deployment — not
// a rebuild and not a hunt through components (FAC-20).
//
// The variables hold space-separated RGB channels, not hex, because that is what
// `<alpha-value>` needs: without it every `bg-brand-500/50` in the app would
// silently stop applying its opacity.
const brandVar = (name) => `rgb(var(--${name}) / <alpha-value>)`;

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        brand: {
          50: brandVar("brand-50"),
          100: brandVar("brand-100"),
          500: brandVar("brand-500"),
          600: brandVar("brand-600"),
          700: brandVar("brand-700"),
        },
      },
    },
  },
  plugins: [],
};
