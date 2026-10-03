import { ssrSettings } from "./server-config.mjs";
const settings = ssrSettings();
process.env.NITRO_HOST = settings.host;
process.env.NITRO_PORT = String(settings.port);
await import("../.output/server/index.mjs");
