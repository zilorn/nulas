import { solidStart } from "@solidjs/start/config";
import { nitro } from "nitro/vite";
import { defineConfig } from "vite";
import { readPorts } from "./scripts/server-config.mjs";

const ports = readPorts();
const backendAddress = process.env.NULAS_ADDR || `127.0.0.1:${ports.port}`;

export default defineConfig({
  plugins: [solidStart({ ssr: true, middleware: "src/middleware.ts" }), nitro()],
  nitro: {
    preset: "node",
    // Nitro handles application requests before Vite's ordinary proxy.
    devProxy: { "/api/**": { target: `http://${backendAddress}`, changeOrigin: false } },
  },
  server: {
    host: "127.0.0.1",
    port: ports["dev-port"],
    strictPort: true,
  },
});
