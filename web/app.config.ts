import { defineConfig } from "@solidjs/start/config";
export default defineConfig({
 ssr: false,
 server: {
  preset: "static",
  prerender: { routes: ["/"] },
  // Proxy before Vinxi's internal router, preserving the browser-facing Host.
  // The mounted /api prefix is stripped, so include it in the target path.
  devProxy: { "/api": { target: "http://127.0.0.1:8080/api", changeOrigin: false } }
 }
});
