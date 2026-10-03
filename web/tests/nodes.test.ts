import assert from "node:assert/strict";
import { test } from "node:test";
import { canChoose, visibleGroups, type Proxy } from "../src/lib/nodes.ts";

const nodes: Proxy[] = [
 { name: "ChatGPT", type: "Selector", all: ["海外使用"] },
 { name: "GLOBAL", type: "Selector", all: ["DIRECT", "节点 A"] },
 { name: "海外使用", type: "URLTest", all: ["节点 A"] },
 { name: "节点 A", type: "Vless" },
];

test("rule mode exposes configured groups without GLOBAL", () => {
 assert.deepEqual(visibleGroups(nodes, "rule").map(p => p.name), ["ChatGPT", "海外使用"]);
 assert.equal(canChoose(nodes[0], "rule"), true);
 assert.equal(canChoose(nodes[1], "rule"), false);
 assert.equal(canChoose(nodes[2], "rule"), false);
});

test("global mode only exposes and selects GLOBAL", () => {
 assert.deepEqual(visibleGroups(nodes, "global").map(p => p.name), ["GLOBAL"]);
 assert.equal(canChoose(nodes[1], "global"), true);
 assert.equal(canChoose(nodes[0], "global"), false);
 assert.deepEqual(visibleGroups(nodes.filter(p => p.name !== "GLOBAL"), "global"), []);
});

test("direct and unknown modes cannot expose or select proxy groups", () => {
 for (const mode of ["direct", "", "unknown"]) {
  assert.deepEqual(visibleGroups(nodes, mode), []);
  for (const group of nodes) assert.equal(canChoose(group, mode), false);
 }
});

test("rule mode places the MATCH target first and preserves other group order", () => {
 const list: Proxy[] = [...nodes, { name: "Other", type: "Selector", all: ["节点 A"] }];
 assert.deepEqual(visibleGroups(list, "rule", "海外使用").map(p => p.name), ["海外使用", "ChatGPT", "Other"]);
 assert.deepEqual(list.map(p => p.name), ["ChatGPT", "GLOBAL", "海外使用", "节点 A", "Other"]);
});

test("missing or non-group MATCH targets preserve the configured order", () => {
 for (const target of [undefined, "DIRECT", "节点 A", "missing", "GLOBAL", "ChatGPT"]) {
  assert.deepEqual(visibleGroups(nodes, "rule", target).map(p => p.name), ["ChatGPT", "海外使用"]);
 }
});

test("MATCH targets do not change other modes", () => {
 assert.deepEqual(visibleGroups(nodes, "global", "海外使用").map(p => p.name), ["GLOBAL"]);
 for (const mode of ["direct", "", "unknown"]) {
  assert.deepEqual(visibleGroups(nodes, mode, "海外使用"), []);
 }
});
