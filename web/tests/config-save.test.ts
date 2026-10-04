import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import ts from "typescript";
import { translate, type Locale } from "../src/lib/translation.ts";

// Exercise the route's actual handler with controlled API responses and UI state.
const source = readFileSync(new URL("../src/routes/index.tsx", import.meta.url), "utf8");
const ast = ts.createSourceFile("index.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
let handler: ts.FunctionDeclaration | undefined;
function visit(node: ts.Node) {
 if (ts.isFunctionDeclaration(node) && node.name?.text === "save") handler = node;
 ts.forEachChild(node, visit);
}
visit(ast);
assert.ok(handler);
const compiled = ts.transpile(handler.getText(ast), { target: ts.ScriptTarget.ESNext });

async function saveScenario(action?: string, failure?: "config" | "jobs", locale: Locale = "zh-CN") {
 const state = { busy: false, notice: "old notice", error: "old error", refreshed: 0 };
 const calls: { path: string; method?: string }[] = [];
 const save = new Function("api", "config", "setBusy", "setNotice", "setError", "t", "refresh", "online", "ready", "busy", "pending", `${compiled}; return save;`)(
  async (path: string, options: RequestInit) => {
   calls.push({ path, method: options.method });
   if (path === failure) throw new Error(path === "jobs" ? "已有待处理任务" : "保存被拒绝");
  },
  () => ({ "mixed-port": 7890 }),
  (value: boolean) => { state.busy = value; },
  (value: string) => { state.notice = value; },
  (value: string) => { state.error = value; },
  (message: string, parameters?: Record<string, string>) => translate(locale, message, parameters),
  async () => { state.refreshed++; },
  () => true,
  () => true,
  () => state.busy,
  () => false,
 );
 await save(action);
 assert.equal(state.busy, false);
 return { state, calls };
}

test("task conflict preserves successful save and identifies the task failure in both languages", async () => {
 for (const locale of ["zh-CN", "en"] as const) {
  for (const action of ["apply", "generate"]) {
   const { state, calls } = await saveScenario(action, "jobs", locale);
   assert.equal(state.notice, translate(locale, "配置已保存。"));
   assert.equal(state.error, translate(locale, "后台任务提交失败：{message}", { message: "已有待处理任务" }));
   assert.equal(state.refreshed, 1);
   assert.deepEqual(calls, [{ path: "config", method: "PUT" }, { path: "jobs", method: "POST" }]);
  }
 }
});

test("failed config save never submits a task or reports success", async () => {
 const { state, calls } = await saveScenario("apply", "config");
 assert.equal(state.notice, "");
 assert.equal(state.error, "保存被拒绝");
 assert.deepEqual(calls, [{ path: "config", method: "PUT" }]);
 assert.equal(state.refreshed, 0);
});

test("save alone reports success without submitting a task", async () => {
 const { state, calls } = await saveScenario();
 assert.equal(state.notice, "配置已保存。");
 assert.equal(state.error, "");
 assert.deepEqual(calls, [{ path: "config", method: "PUT" }]);
 assert.equal(state.refreshed, 1);
});

test("successful save and submission reports both outcomes", async () => {
 const { state } = await saveScenario("apply");
 assert.equal(state.notice, "配置已保存。后台任务已提交，可在后台任务页查看结果，关闭网页后仍会继续执行。");
 assert.equal(state.error, "");
 assert.equal(state.refreshed, 1);
});
