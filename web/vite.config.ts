import { solidStart } from "@solidjs/start/config";
import { nitro } from "nitro/vite";
import { defineConfig } from "vite";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { readPorts } from "./scripts/server-config.mjs";

const ports = readPorts();
const backendAddress = process.env.NULAS_ADDR || `127.0.0.1:${ports.port}`;
function readBuildHash() {
  const override = process.env.NULAS_BUILD_HASH?.trim();
  if (override) {
    if (!/^[a-f0-9]{7,40}$/i.test(override)) throw new Error("NULAS_BUILD_HASH must be a Git commit hash (7–40 hexadecimal characters).");
    return override;
  }
  try {
    return execFileSync("git", ["rev-parse", "--verify", "HEAD"], {
      cwd: fileURLToPath(new URL("../", import.meta.url)),
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return "";
  }
}

export default defineConfig({
  define: { __NULAS_BUILD_HASH__: JSON.stringify(readBuildHash()) },
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
