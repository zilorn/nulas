import assert from "node:assert/strict";
import { test } from "node:test";
import { readFileSync, readdirSync } from "node:fs";
import ts from "typescript";
import { en } from "../src/lib/locales/en.ts";
import { resolveLocale, savedLocale, translate } from "../src/lib/translation.ts";

test("language resolution honors priority and handles unsupported or invalid choices", () => {
 assert.equal(resolveLocale(["fr", "en-GB", "zh-CN"]), "en");
 assert.equal(resolveLocale(["zh-TW", "en"]), "zh-CN");
 assert.equal(resolveLocale(["english", "fr"]), "zh-CN");
 assert.equal(resolveLocale([]), "zh-CN");
 assert.equal(savedLocale("other=en; nulas-language=en"), "en");
 assert.equal(savedLocale("nulas-language=zh-CN; other=x"), "zh-CN");
 assert.equal(savedLocale("nulas-language=fr"), undefined);
 assert.equal(savedLocale("fake-nulas-language=en"), undefined);
});

test("translation preserves fallback and does not reinterpret parameter contents", () => {
 assert.equal(translate("en", "快速配置"), "Quick configuration");
 assert.equal(translate("zh-CN", "快速配置"), "快速配置");
 assert.equal(translate("en", "unknown"), "unknown");
 assert.equal(translate("en", "toString"), "toString");
 assert.equal(translate("en", "正在测速 {p0} / {p1}", { p0: "{p1}", p1: 5 }), "Testing {p1} / 5");
 assert.equal(translate("zh-CN", "{value}", { value: "<script>$&</script>" }), "<script>$&</script>");
 assert.equal(translate("en", "测速 {p0}"), "Test {p0}");
});

const placeholders = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map(match => match[1]).sort();
test("all translations preserve parameters and contain translated text", () => {
 for (const [source, translated] of Object.entries(en)) {
  assert.ok(translated.trim(), source);
  assert.deepEqual(placeholders(translated), placeholders(source), source);
  assert.ok(!/[\u3400-\u9fff]/.test(translated), source);
 }
});

test("Chinese UI literals and literal translation calls have dictionary entries", () => {
 for (const directory of ["components", "routes", "lib"]) {
  for (const entry of readdirSync(new URL(`../src/${directory}/`, import.meta.url), { recursive: true, encoding: "utf8" })) {
   const file = entry.replaceAll("\\", "/");
   // Dictionary source keys are declarations, not UI literals.
   if (directory === "lib" && file.startsWith("locales/")) continue;
   if (!/\.tsx?$/.test(file)) continue;
   const source = readFileSync(new URL(`../src/${directory}/${file}`, import.meta.url), "utf8");
   const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
   function visit(node: ts.Node) {
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && /[\u3400-\u9fff]/.test(node.text)) assert.ok(Object.hasOwn(en, node.text), `${file}: ${node.text}`);
    if (ts.isTemplateExpression(node)) assert.ok(!/[\u3400-\u9fff]/.test(node.head.text + node.templateSpans.map(span => span.literal.text).join("")), `${file}: use a parameterized translation for Chinese template text`);
    if (ts.isJsxText(node) && !(ts.isJsxElement(node.parent) && node.parent.openingElement.tagName.getText(ast) === "option" && node.text === "简体中文")) assert.ok(!/[\u3400-\u9fff]/.test(node.text), `${file}: untranslated JSX text`);
    if (ts.isCallExpression(node) && node.expression.getText(ast) === "t" && ts.isStringLiteral(node.arguments[0])) assert.ok(Object.hasOwn(en, node.arguments[0].text), `${file}: missing translation`);
    ts.forEachChild(node, visit);
   }
   visit(ast);
  }
 }
});
