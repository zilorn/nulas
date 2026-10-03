import { defineConfig } from "@solidjs/start/config";
export default defineConfig({ ssr: false, server: { preset: "static", prerender: { routes: ["/"] } }, vite: { server: { proxy: { "/api": "http://127.0.0.1:8080" } } } });
