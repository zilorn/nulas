import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import ts from "typescript";
import { startVisiblePolling } from "../src/lib/polling.ts";

class Page extends EventTarget {
 hidden = false;
 show(hidden: boolean) { this.hidden = hidden; this.dispatchEvent(new Event("visibilitychange")); }
}
const settle = async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); };

test("polling pauses when hidden, resumes immediately and cleans up", async context => {
 context.mock.timers.enable({ apis: ["setInterval"] });
 const page = new Page();
 page.hidden = true;
 let calls = 0;
 const stop = startVisiblePolling(async () => { calls++; }, 2000, page);
 context.mock.timers.tick(10000); await settle();
 assert.equal(calls, 0);
 page.show(false); await settle();
 assert.equal(calls, 1);
 context.mock.timers.tick(2000); await settle();
 assert.equal(calls, 2);
 page.show(true);
 context.mock.timers.tick(10000); await settle();
 assert.equal(calls, 2);
 page.show(false); await settle();
 assert.equal(calls, 3);
 stop(); page.show(false);
 context.mock.timers.tick(10000); await settle();
 assert.equal(calls, 3);
});

test("slow polls cannot overlap, even across visibility changes", async context => {
 context.mock.timers.enable({ apis: ["setInterval"] });
 const page = new Page();
 let calls = 0;
 let complete!: () => void;
 const stop = startVisiblePolling(async () => { calls++; await new Promise<void>(resolve => { complete = resolve; }); }, 2000, page);
 context.mock.timers.tick(10000); page.show(true); page.show(false);
 assert.equal(calls, 1);
 complete(); await settle();
 context.mock.timers.tick(2000);
 assert.equal(calls, 2);
 stop(); complete(); await settle();
 context.mock.timers.tick(10000);
 assert.equal(calls, 2);
});

function extract(file: string, find: (node: ts.Node) => boolean) {
 const source = readFileSync(new URL(file, import.meta.url), "utf8");
 const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
 let result: ts.Node | undefined;
 function visit(node: ts.Node) { if (find(node)) result = node; ts.forEachChild(node, visit); }
 visit(ast); assert.ok(result);
 return ts.transpile(result.getText(ast), { target: ts.ScriptTarget.ESNext });
}
const loadSource = extract("../src/routes/index.tsx", node => ts.isFunctionDeclaration(node) && node.name?.text === "loadConfig");
function configScenario(api: () => Promise<unknown>) {
 const state = { ready: false, loading: false, error: "", config: {} as unknown };
 const load = new Function("api", "ready", "configLoading", "setConfigLoading", "setConfig", "setReady", "setConfigError", `${loadSource}; return loadConfig;`)(
  api, () => state.ready, () => state.loading,
  (v: boolean) => { state.loading = v; }, (v: unknown) => { state.config = v; },
  (v: boolean) => { state.ready = v; }, (v: string) => { state.error = v; },
 );
 return { state, load };
}

test("failed initial config load recovers and never reloads over edits", async () => {
 let calls = 0;
 const { state, load } = configScenario(async () => { if (++calls === 1) throw new Error("offline"); return { mode: "rule" }; });
 await load();
 assert.equal(state.ready, false); assert.equal(state.error, "offline"); assert.equal(state.loading, false);
 await load();
 assert.equal(state.ready, true); assert.equal(state.error, "");
 state.config = { mode: "direct" }; await load();
 assert.equal(calls, 2); assert.deepEqual(state.config, { mode: "direct" });
});

test("config retries cannot overlap or overwrite a newly loaded profile", async () => {
 let resolve!: (value: unknown) => void;
 let calls = 0;
 const { state, load } = configScenario(() => { calls++; return new Promise(r => { resolve = r; }); });
 const first = load(); await load();
 assert.equal(calls, 1);
 state.ready = true; state.config = { mode: "global" };
 resolve({ mode: "rule" }); await first;
 assert.deepEqual(state.config, { mode: "global" }); assert.equal(state.loading, false);
});

const pollSource = extract("../src/components/ConfigurationManager.tsx", node => ts.isCallExpression(node) && node.expression.getText() === "usePolling");
test("transient library errors retain controller state and clear on recovery", async () => {
 let controller = true;
 let failure = true;
 let error = "";
 let loading = true;
 let poll!: () => Promise<void>;
 new Function("usePolling", "refresh", "setPollError", "setLoading", "setController", "t", pollSource)(
  (callback: () => Promise<void>) => { poll = callback; },
  async () => { if (failure) throw new Error("temporary"); controller = false; },
  (v: string) => { error = v; }, (v: boolean) => { loading = v; },
  (v: boolean) => { controller = v; }, (message: string) => message,
 );
 await poll(); assert.equal(controller, true); assert.ok(error); assert.equal(loading, false);
 failure = false; await poll(); assert.equal(controller, false); assert.equal(error, "");
});

const refreshSource = extract("../src/components/ConfigurationManager.tsx", node => ts.isVariableDeclaration(node) && node.name.getText() === "refresh");
test("a failed library request waits for slow siblings before another poll", async () => {
 let finish!: () => void;
 let settled = false;
 let updates = 0;
 const refresh = new Function("api", "setProfiles", "setController", "setApplied", `const ${refreshSource}; return refresh;`)(
  async (path: string) => {
   if (path === "profiles") throw new Error("temporary");
   if (path === "health") return { controllerConfigured: true };
   await new Promise<void>(resolve => { finish = resolve; }); return null;
  },
  () => { updates++; }, () => { updates++; }, () => { updates++; },
 );
 const pending = refresh().catch((e: Error) => { assert.equal(e.message, "temporary"); settled = true; });
 await settle(); assert.equal(settled, false);
 finish(); await pending; assert.equal(settled, true); assert.equal(updates, 0);
});
