import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { configPath, readPorts, ssrSettings } from "../scripts/server-config.mjs";

test("saved ports, legacy files and invalid values", () => {
  const dir = mkdtempSync(join(tmpdir(), "nulas-ports-"));
  const path = join(dir, "server.json");
  try {
    assert.deepEqual(readPorts(path), { port: 4669, "ssr-port": 4668, "dev-port": 4589 });
    writeFileSync(path, JSON.stringify({ port: 8181, future: true }));
    assert.deepEqual(readPorts(path), { port: 8181, "ssr-port": 4668, "dev-port": 4589 });
    writeFileSync(path, JSON.stringify({ port: 8181, "ssr-port": 8282, "dev-port": 8383 }));
    const ports = readPorts(path);
    assert.deepEqual(ssrSettings(ports, {}), { host: "127.0.0.1", port: 8282 });
    assert.deepEqual(ssrSettings(ports, { NULAS_SSR_URL: "http://[::1]:8484/" }), { host: "::1", port: 8484 });
    assert.throws(() => ssrSettings(ports, { NULAS_SSR_URL: "http://example.com:8484" }));
    assert.throws(() => ssrSettings(ports, { NULAS_SSR_URL: "http://127.0.0.1:8181" }));
    for (const key of ["port", "ssr-port", "dev-port"]) {
      for (const value of [null, 0, 65536, "80", 1.5]) {
        writeFileSync(path, JSON.stringify({ [key]: value }));
        assert.throws(() => readPorts(path));
      }
    }
    for (const data of ["null", "[]", "{broken"]) {
      writeFileSync(path, data);
      assert.throws(() => readPorts(path));
    }
  } finally { rmSync(dir, { recursive: true }); }
});

test("user config directory matches each platform", () => {
  assert.equal(configPath({ XDG_CONFIG_HOME: "/config" }, "linux", "/home/user"), "/config/nulas/server.json");
  assert.equal(configPath({}, "linux", "/home/user"), "/home/user/.config/nulas/server.json");
  assert.equal(configPath({}, "darwin", "/Users/user"), "/Users/user/Library/Application Support/nulas/server.json");
  assert.equal(configPath({ AppData: "/appdata" }, "win32", "/user"), "/appdata/nulas/server.json");
  assert.throws(() => configPath({}, "win32", "/user"));
});
