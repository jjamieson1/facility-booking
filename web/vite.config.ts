import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The SPA is served under /facility-booking/ in production (Apache base path)
// and at / in dev. Set VITE_BASE to match the deployment base path.
// In dev, /api and /healthz proxy to the Go API so cookies are same-origin;
// point VITE_API_TARGET at the API if it isn't on the default :8080.
// Port 5180 (not 5173) so we coexist with C2's SPA. The API is on :8086.
const apiTarget = process.env.VITE_API_TARGET ?? "http://localhost:8086";

export default defineConfig({
  base: process.env.VITE_BASE ?? "/",
  plugins: [react()],
  server: {
    port: 5180,
    strictPort: true,
    proxy: {
      "/api": apiTarget,
      "/healthz": apiTarget,
    },
  },
});
