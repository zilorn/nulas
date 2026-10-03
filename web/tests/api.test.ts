import assert from "node:assert/strict";
import { test } from "node:test";
import { api } from "../src/lib/api.ts";

test("API rejects HTML, malformed JSON and preserves backend errors", async (t) => {
 const cases = [
  { response: new Response("<!DOCTYPE html><html></html>", { headers: { "Content-Type": "text/html" } }), error: /Web\/API 入口/ },
  { response: new Response("{broken", { headers: { "Content-Type": "application/json" } }), error: /无效的 JSON/ },
  { response: Response.json({ error: "SSR 内部端口" }, { status: 503 }), error: /SSR 内部端口/ },
  { response: Response.json(null, { status: 502 }), error: /HTTP 502/ },
 ];
 for (const { response, error } of cases) {
  t.mock.method(globalThis, "fetch", async () => response);
  await assert.rejects(api("health"), error);
  t.mock.restoreAll();
 }
});

test("API preserves same-origin paths, request options and JSON responses", async (t) => {
 const options = { method: "POST", body: "{}" };
 t.mock.method(globalThis, "fetch", async (path: string, init?: RequestInit) => {
  assert.equal(path, "/api/jobs");
  assert.equal(init, options);
  return Response.json({ id: "job-1" });
 });
 assert.deepEqual(await api("jobs", options), { id: "job-1" });
});
