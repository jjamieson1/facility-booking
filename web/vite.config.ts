import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

import { documentTitle } from "./src/lib/brand";

// index.html is static, so it cannot read brand.ts — which made its <title> the
// one place a rebrand still had to be remembered, and the one place the brand
// guard cannot see (it walks .ts/.tsx). This substitutes it at build time from
// the same single source the app uses.
//
// It throws rather than silently doing nothing if the tag is gone: a title that
// quietly stops being substituted is exactly the kind of drift FAC-20 exists to
// prevent, and a build failure naming the file is cheaper than discovering it
// on a municipality's tab.
function brandTitle(): Plugin {
  const titleTag = /<title>[^<]*<\/title>/;
  return {
    name: "brand-title",
    transformIndexHtml(html) {
      if (!titleTag.test(html)) {
        throw new Error(
          "vite.config.ts brandTitle: no <title> tag in index.html to substitute. " +
            "The page title must come from src/lib/brand.ts (FAC-20); restore the tag " +
            "or remove this plugin deliberately.",
        );
      }
      return html.replace(titleTag, `<title>${documentTitle()}</title>`);
    },
  };
}

// The SPA is served under /facility-booking/ in production (Apache base path)
// and at / in dev. Set VITE_BASE to match the deployment base path.
// In dev, /api and /healthz proxy to the Go API so cookies are same-origin;
// point VITE_API_TARGET at the API if it isn't on the default :8080.
// Port 5180 (not 5173) so we coexist with C2's SPA. The API is on :8086.
const apiTarget = process.env.VITE_API_TARGET ?? "http://localhost:8086";

export default defineConfig({
  base: process.env.VITE_BASE ?? "/",
  plugins: [react(), brandTitle()],
  server: {
    port: 5180,
    strictPort: true,
    proxy: {
      "/api": apiTarget,
      "/healthz": apiTarget,
    },
  },
});
