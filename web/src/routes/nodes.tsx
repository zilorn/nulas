import { useI18n } from "../lib/i18n";
import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";
import Workspace from "../components/Workspace";
import { createApi } from "../lib/api";
import { canChoose, canTest, visibleGroups, type Proxy } from "../lib/nodes";

const modes: Record<string, string> = { rule: "规则模式", global: "全局模式", direct: "直连模式" };

export default function Nodes() {
 const { t, locale } = useI18n();
 const api = createApi(t);
 const [nodes, setNodes] = createSignal<Proxy[]>([]);
 const [mode, setMode] = createSignal("");
 const [ready, setReady] = createSignal(false);
 const [busy, setBusy] = createSignal(false);
 const [error, setError] = createSignal("");
 const [notice, setNotice] = createSignal("");
 const [search, setSearch] = createSignal("");
 const [activeName, setActiveName] = createSignal("");
 const [switching, setSwitching] = createSignal<{ group: string; name: string }>();
 const [fallback, setFallback] = createSignal<{ target: string; configured: boolean }>();
 const [fallbackError, setFallbackError] = createSignal("");
 const [testing, setTesting] = createSignal(false);
 const [progress, setProgress] = createSignal({ done: 0, total: 0 });
 const [delays, setDelays] = createSignal<Record<string, { status: "waiting" | "testing" | "done" | "failed"; delay?: number; error?: string; time?: string }>>({});
 let testController: AbortController | undefined;
 onCleanup(() => testController?.abort());

 async function testNodes(names: string[]) {
  if (busy() || testing() || !ready()) return;
  const targets = [...new Set(names)].filter(name => canTest(nodes().find(p => p.name === name)));
  if (!targets.length) return;
  const controller = new AbortController();
  testController = controller;
  setTesting(true); setProgress({ done: 0, total: targets.length });
  setDelays(previous => ({ ...previous, ...Object.fromEntries(targets.map(name => [name, { status: "waiting" as const }])) }));
  let index = 0;
  async function worker() {
   while (index < targets.length && !controller.signal.aborted) {
    const name = targets[index++];
    setDelays(previous => ({ ...previous, [name]: { status: "testing" } }));
    try {
     const result = await api<{ name: string; delay: number }>("nodes/delay", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name }), signal: controller.signal });
     if (!controller.signal.aborted) setDelays(previous => ({ ...previous, [name]: { status: "done", delay: result.delay, time: new Date().toLocaleTimeString(locale()) } }));
    } catch (e) {
     if (!controller.signal.aborted) setDelays(previous => ({ ...previous, [name]: { status: "failed", error: (e as Error).message } }));
    } finally {
     if (!controller.signal.aborted) setProgress(previous => ({ ...previous, done: previous.done + 1 }));
    }
   }
  }
  try { await Promise.all(Array.from({ length: Math.min(4, targets.length) }, worker)); }
  finally { if (!controller.signal.aborted) setTesting(false); }
 }

 const delayLabel = (name: string) => {
  const value = delays()[name];
  if (!value) return t("未测速");
  return value.status === "waiting" ? t("等待测速") : value.status === "testing" ? t("测速中…") : value.status === "failed" ? t("测速失败") : `${value.delay} ms`;
 };

 async function refreshFallback() {
  setFallback(undefined); setFallbackError("");
  try { setFallback(await api("runtime/fallback")); }
  catch (e) { setFallbackError(t("兜底出口读取失败：{p0}", { p0: (e as Error).message })); }
 }

 async function refresh() {
  if (busy() || testing()) return;
  setBusy(true); setError(""); setNotice("");
  try {
   const [list, runtime] = await Promise.all([api<Proxy[]>("nodes"), api<{ mode: string }>("runtime/mode")]);
   setNodes(list); setDelays({}); setProgress({ done: 0, total: 0 }); setMode(runtime.mode); setReady(true);
   if (runtime.mode === "rule") await refreshFallback();
  } catch (e) { setReady(false); setError((e as Error).message); }
  finally { setBusy(false); }
 }
 onMount(() => void refresh());

 async function changeMode(value: string) {
  if (busy() || testing() || value === mode()) return;
  setBusy(true); setNotice(""); setError("");
  try {
   const result = await api<{ mode: string }>("runtime/mode", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ mode: value }) });
   setMode(result.mode);
   setSearch("");
   if (result.mode === "rule") await refreshFallback();
   setNotice(t("内核已切换为{p0}。", { p0: t(modes[result.mode] || result.mode) }));
  } catch (e) { setError((e as Error).message); }
  finally { setBusy(false); }
 }

 async function choose(group: Proxy, name: string) {
  if (busy() || testing() || !ready() || !canChoose(group, mode()) || group.now === name) return;
  setBusy(true); setSwitching({ group: group.name, name }); setError(""); setNotice("");
  try {
   await api("nodes/selection", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ group: group.name, name }) });
   setNodes(nodes().map(p => p.name === group.name ? { ...p, now: name } : p));
   setNotice(t("「{p0}」已切换至「{p1}」。", { p0: group.name, p1: name }));
  } catch (e) { setError(t("切换失败：{p0}", { p0: (e as Error).message })); }
  finally { setSwitching(undefined); setBusy(false); }
 }

 const query = createMemo(() => search().trim().toLowerCase());
 const groups = createMemo(() => visibleGroups(nodes(), mode(), fallback()?.target));
 const filtered = createMemo(() => groups().filter(p => p.name.toLowerCase().includes(query()) || p.all?.some(n => n.toLowerCase().includes(query()))));
 const activeGroup = createMemo(() => filtered().find(p => p.name === activeName()) || filtered().find(p => p.type === "Selector") || filtered()[0]);
 const members = (group: Proxy) => group.name.toLowerCase().includes(query()) ? group.all || [] : (group.all || []).filter(name => name.toLowerCase().includes(query()));
 const proxyTypes = createMemo(() => new Map(nodes().map(p => [p.name, p.type])));
 const fallbackGroup = createMemo(() => groups().find(p => p.name === fallback()?.target));
 const pending = (group: Proxy, name: string) => switching()?.group === group.name && switching()?.name === name;

 return <Workspace page="nodes">
  <section class="intro"><div><span class="eyebrow">PROXY NODES</span><h1>{t("节点管理")}<span>{t("，点选即切换。")}</span></h1><p>{t("选择代理组，点击节点卡片即可使用。当前使用的节点会保持高亮。")}</p></div><button class="secondary" disabled={busy() || testing()} onClick={() => void refresh()}>{busy() ? t("正在处理…") : t("刷新节点与模式")}</button></section>
  <Show when={error()}><p class="error" role="alert">{error()}</p></Show>
  <Show when={notice()}><p class="success" role="status">{notice()}</p></Show>
  <section class="panel mode-panel">
   <div class="panel-heading"><div><h2>{t("内核运行模式")}</h2><p>{t("当前模式：")}{t(modes[mode()] || mode()) || t("未连接")}{t("。切换立即生效。")}</p></div></div>
   <div class="mode-options"><For each={Object.entries(modes)}>{([value, label]) => <button class={mode() === value ? "" : "secondary"} aria-pressed={mode() === value} disabled={busy() || testing() || !ready()} onClick={() => void changeMode(value)}>{t(label)}<small>{({ rule: t("按内核规则选择出站"), global: t("使用 GLOBAL 组选择的节点"), direct: t("全部流量直接连接") } as Record<string, string>)[value]}</small></button>}</For></div>
   <p>{t("节点和模式切换作用于当前内核；重启或重新加载配置后可能恢复原配置。")}</p>
   <Show when={ready() && mode() === "rule"}>
    <div class="rule-fallback"><div><h3>{t("未命中规则时的出口")}</h3><Show when={fallback()} fallback={<p role="status">{fallbackError() || t("正在读取兜底规则…")}</p>}>{value => <><p><span class="rule-fallback-route">{value().configured ? t("未命中前面的规则时，按 MATCH 规则路由到：") : t("未配置有效 MATCH 规则；所有规则均未命中时，内核默认直连：")}<strong>{value().target}</strong></span><Show when={fallbackGroup()?.now}><span class="rule-fallback-current">{t("当前选择：")}{fallbackGroup()?.now}</span></Show></p><small>{fallbackGroup() ? t("可进入此代理组选择成员；自动策略由内核选择。") : t("此出口由内核规则配置决定。更改兜底目标需修改配置中的 MATCH 规则后重新应用。")}</small></>}</Show></div><Show when={fallbackGroup()}><button class="secondary" disabled={busy() || testing()} onClick={() => { setSearch(""); setActiveName(fallbackGroup()!.name); }}>{t("查看兜底代理组")}</button></Show></div>
   </Show>
  </section>
  <section class="panel node-panel">
   <div class="panel-heading library-heading"><div><h2>{t("代理组与节点")}</h2><p>{t("规则模式选择对应代理组；全局模式选择 GLOBAL 组。")}</p></div><Show when={mode() !== "direct"}><label class="search-label">{t("搜索节点 / 代理组")}<input type="search" value={search()} onInput={e => setSearch(e.currentTarget.value)} placeholder={t("输入名称，快速找到节点")} /></label></Show></div>
   <Show when={ready()} fallback={<div class="empty"><p>{busy() ? t("正在读取内核配置…") : t("请连接内核后刷新")}</p></div>}>
    <Show when={mode() !== "direct"} fallback={<div class="empty"><p>{t("直连模式")}</p><small>{t("全部流量直接连接。切换为规则或全局模式后可选择代理节点。")}</small></div>}>
    <Show when={filtered().length} fallback={<div class="empty">◇<p>{query() ? t("没有匹配的节点或代理组") : mode() === "global" ? t("当前内核没有可选的 GLOBAL 组") : t("当前内核没有可选代理组")}</p><Show when={query()} fallback={<small>{t("请在 Mihomo 加载包含节点与代理组的配置，再刷新此页面。托管内核默认仅提供直连。")}</small>}><button class="secondary" onClick={() => setSearch("")}>{t("清空搜索")}</button></Show></div>}>
     <div class="node-workspace">
      <nav class="node-groups" aria-label={t("代理组")} tabindex="0"><div class="node-groups-heading">{t("代理组")}<span>{filtered().length}</span></div><For each={filtered()}>{group => <button class="node-group" classList={{ "is-active": activeGroup()?.name === group.name }} aria-pressed={activeGroup()?.name === group.name} onClick={() => setActiveName(group.name)}><span class="node-group-title">{group.name}<span class="node-count">{group.all?.length}</span></span><small>{group.type === "Selector" ? t("手动选择") : t("自动策略")} · {group.now || t("尚无当前节点")}</small></button>}</For></nav>
      <Show when={activeGroup()}>{group => <div class="node-browser" role="region" aria-label={t("节点列表")} tabindex="0" aria-busy={!!switching()}>
       <div class="node-browser-heading"><div><h3>{group().name}</h3><p>{group().type === "Selector" ? t("点击下方卡片，立即切换节点") : t("由内核自动选择，以下节点仅供查看")}</p></div><span class="badge">{group().type === "Selector" ? t("手动选择") : group().type}</span></div>
       <div class="node-current"><span class="node-current-dot" aria-hidden="true"/><div><small>{t("当前使用")}</small><strong>{group().now || t("由内核自动分配")}</strong></div></div>
       <p class="node-test-hint">{t("测速为连接延迟，并非下载带宽。目标：gstatic generate_204；超时 5 秒。代理组按当前策略测速，结果仅反映本次测试。")}</p>
       <div class="node-test-toolbar"><button class="secondary" disabled={busy() || testing() || !members(group()).some(name => canTest(nodes().find(p => p.name === name)))} onClick={() => void testNodes(members(group()))}>{query() ? t("测速搜索结果") : t("测速当前组")}</button><span role="status">{testing() ? t("正在测速 {p0} / {p1}", { p0: progress().done, p1: progress().total }) : progress().total ? t("测速完成 {p0} / {p1}", { p0: progress().done, p1: progress().total }) : t("支持单个节点或批量测速")}</span></div>
       <div class="node-list-heading"><span>{query() ? t("找到 {p0} / {p1} 个节点 / 策略", { p0: members(group()).length, p1: group().all?.length }) : t("{p0} 个节点 / 策略", { p0: group().all?.length })}</span><Show when={query()}><button class="node-clear" onClick={() => setSearch("")}>{t("清空搜索")}</button></Show></div>
       <div class="node-grid"><For each={members(group())}>{name => <div class="node-card"><Show when={group().type === "Selector"} fallback={<div class="node-tile node-tile-readonly" classList={{ "is-current": group().now === name }}><span class="node-tile-name">{name}</span><span class="node-tile-meta"><span>{proxyTypes().get(name) || t("代理")}</span><span>{group().now === name ? t("✓ 当前使用") : t("自动策略")}</span></span></div>}>
        <button class="node-tile" classList={{ "is-current": group().now === name, "is-switching": pending(group(), name) }} aria-pressed={group().now === name} aria-label={t("{p0}，{p1}", { p0: name, p1: group().now === name ? t("当前使用") : t("点击切换") })} disabled={busy() || testing()} onClick={() => void choose(group(), name)}><span class="node-tile-name">{name}</span><span class="node-tile-meta"><span>{proxyTypes().get(name) || t("代理")}</span><span>{pending(group(), name) ? t("切换中…") : group().now === name ? t("✓ 当前使用") : t("点击使用 →")}</span></span></button>
       </Show><div class="node-test-row"><span classList={{ "node-delay-failed": delays()[name]?.status === "failed" }} title={delays()[name]?.error || (delays()[name]?.time ? t("测试时间：{p0}", { p0: delays()[name].time }) : undefined)} aria-live="polite">{canTest(nodes().find(p => p.name === name)) ? delayLabel(name) : t("不支持测速")}</span><button class="secondary node-test-button" aria-label={t("测速 {p0}", { p0: name })} disabled={busy() || testing() || !canTest(nodes().find(p => p.name === name))} onClick={() => void testNodes([name])}>{t("测速")}</button></div><Show when={delays()[name]?.error}><small class="node-test-error">{delays()[name]?.error}</small></Show></div>}</For></div>
      </div>}</Show>
     </div>
    </Show>
    </Show>
   </Show>
  </section>
 </Workspace>;
}
