import assert from "node:assert/strict";
import { test } from "node:test";
import { api, createApi } from "../src/lib/api.ts";

import { translate, type Locale } from "../src/lib/translation.ts";

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


test("API client errors use the selected language and backend errors remain raw", async (t) => {
 const cases = [
  { response: () => new Response("<html></html>", { headers: { "Content-Type": "text/html" } }), message: "API 返回了非 JSON 响应，请使用生产 Web/API 入口（通过 nulas config port 查询），并确认 Go 后端已启动；SSR 端口不提供 API。" },
  { response: () => new Response("{broken", { headers: { "Content-Type": "application/json" } }), message: "API 返回了无效的 JSON 响应，请检查后端服务。" },
  { response: () => Response.json(null, { status: 502 }), message: "请求失败（HTTP {status}）", parameters: { status: 502 } },
 ];
 for (const locale of ["zh-CN", "en"] as const) {
  const request = createApi((message, parameters) => translate(locale, message, parameters));
  for (const entry of cases) {
   t.mock.method(globalThis, "fetch", async () => entry.response());
   await assert.rejects(request("health"), { message: translate(locale, entry.message, entry.parameters) });
   t.mock.restoreAll();
  }
  // Even backend prose matching a dictionary key must not be translated.
  t.mock.method(globalThis, "fetch", async () => Response.json({ error: "快速配置" }, { status: 503 }));
  await assert.rejects(request("health"), { message: "快速配置" });
  t.mock.restoreAll();
 }
});

test("bound API clients read language changes without sharing locale state", async (t) => {
 let locale: Locale = "zh-CN";
 const request = createApi((message, parameters) => translate(locale, message, parameters));
 const chineseRequest = createApi((message, parameters) => translate("zh-CN", message, parameters));
 t.mock.method(globalThis, "fetch", async () => Response.json(null, { status: 502 }));
 await assert.rejects(request("health"), { message: "请求失败（HTTP 502）" });
 locale = "en";
 await assert.rejects(request("health"), { message: "Request failed (HTTP 502)" });
 await assert.rejects(chineseRequest("health"), { message: "请求失败（HTTP 502）" });
});
