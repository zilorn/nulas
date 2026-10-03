import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

// Match Go's os.UserConfigDir, including platform-specific directory selection.
export function configPath(environment = process.env, platform = process.platform, home = homedir()) {
  let directory;
  if (platform === "win32") directory = environment.AppData;
  else if (platform === "darwin") directory = join(home, "Library", "Application Support");
  else directory = environment.XDG_CONFIG_HOME || join(home, ".config");
  if (!directory) throw new Error("Cannot determine the user config directory");
  return join(directory, "nulas", "server.json");
}

export function readPorts(path = configPath()) {
  const defaults = JSON.parse(readFileSync(new URL("../../backend/ports.json", import.meta.url), "utf8"));
  let saved = {};
  try { saved = JSON.parse(readFileSync(path, "utf8")); }
  catch (error) { if (error.code !== "ENOENT") throw error; }
  if (!saved || typeof saved !== "object" || Array.isArray(saved)) throw new Error("Server config must be a JSON object");
  for (const key of Object.keys(defaults)) {
    const value = Object.hasOwn(saved, key) ? saved[key] : defaults[key];
    if (!Number.isInteger(value) || value < 1 || value > 65535) throw new Error(`${key} must be an integer between 1 and 65535`);
    defaults[key] = value;
  }
  return defaults;
}

export function ssrSettings(ports = readPorts(), environment = process.env) {
  const origin = new URL(environment.NULAS_SSR_URL || `http://127.0.0.1:${ports["ssr-port"]}`);
  const host = origin.hostname.replace(/^\[|\]$/g, "");
  if (origin.protocol !== "http:" || !(/^(127\.\d+\.\d+\.\d+|::1)$/.test(host)) || origin.username || origin.password || origin.pathname !== "/" || origin.search || origin.hash) {
    throw new Error("NULAS_SSR_URL must be a loopback HTTP origin");
  }
  const port = Number(origin.port || 80);
  if (port < 1 || port > 65535) throw new Error("SSR port must be between 1 and 65535");
  const webOrigin = new URL(`http://${environment.NULAS_ADDR || `127.0.0.1:${ports.port}`}`);
  if (Number(webOrigin.port || 80) === port) throw new Error("Web/API and SSR ports must be different");
  return { host, port };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const settings = ssrSettings();
  if (process.argv[2] === "ssr-host") console.log(settings.host);
  else if (process.argv[2] === "ssr-port") console.log(settings.port);
  else throw new Error("Expected ssr-host or ssr-port");
}
