import { solidStart } from "@solidjs/start/config";
import { nitro } from "nitro/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [solidStart({ ssr: true }), nitro()],
  nitro: {
    preset: "node",
    // Nitro handles application requests before Vite's ordinary proxy.
    devProxy: { "/api/**": { target: "http://127.0.0.1:8080", changeOrigin: false } },
  },
  server: {
    host: "127.0.0.1",
    port: 3000,
    strictPort: true,
  },
});
