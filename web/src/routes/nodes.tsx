import { createMemo, createSignal, For, onMount, Show } from "solid-js";
import Workspace from "../components/Workspace";
import { api } from "../lib/api";
import { canChoose, visibleGroups, type Proxy } from "../lib/nodes";

const modes: Record<string, string> = { rule: "规则模式", global: "全局模式", direct: "直连模式" };

export default function Nodes() {
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

 async function refreshFallback() {
  setFallback(undefined); setFallbackError("");
  try { setFallback(await api("runtime/fallback")); }
  catch (e) { setFallbackError(`兜底出口读取失败：${(e as Error).message}`); }
 }

 async function refresh() {
  if (busy()) return;
  setBusy(true); setError(""); setNotice("");
  try {
   const [list, runtime] = await Promise.all([api<Proxy[]>("nodes"), api<{ mode: string }>("runtime/mode")]);
   setNodes(list); setMode(runtime.mode); setReady(true);
   if (runtime.mode === "rule") await refreshFallback();
  } catch (e) { setReady(false); setError((e as Error).message); }
  finally { setBusy(false); }
 }
 onMount(() => void refresh());

 async function changeMode(value: string) {
  if (busy() || value === mode()) return;
  setBusy(true); setNotice(""); setError("");
  try {
   const result = await api<{ mode: string }>("runtime/mode", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ mode: value }) });
   setMode(result.mode);
   setSearch("");
   if (result.mode === "rule") await refreshFallback();
   setNotice(`内核已切换为${modes[result.mode]}。`);
  } catch (e) { setError((e as Error).message); }
  finally { setBusy(false); }
 }

 async function choose(group: Proxy, name: string) {
  if (busy() || !ready() || !canChoose(group, mode()) || group.now === name) return;
  setBusy(true); setSwitching({ group: group.name, name }); setError(""); setNotice("");
  try {
   await api("nodes/selection", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ group: group.name, name }) });
   setNodes(nodes().map(p => p.name === group.name ? { ...p, now: name } : p));
   setNotice(`「${group.name}」已切换至「${name}」。`);
  } catch (e) { setError(`切换失败：${(e as Error).message}`); }
  finally { setSwitching(undefined); setBusy(false); }
 }

 const query = createMemo(() => search().trim().toLowerCase());
 const groups = createMemo(() => visibleGroups(nodes(), mode()));
 const filtered = createMemo(() => groups().filter(p => p.name.toLowerCase().includes(query()) || p.all?.some(n => n.toLowerCase().includes(query()))));
 const activeGroup = createMemo(() => filtered().find(p => p.name === activeName()) || filtered().find(p => p.type === "Selector") || filtered()[0]);
 const members = (group: Proxy) => group.name.toLowerCase().includes(query()) ? group.all || [] : (group.all || []).filter(name => name.toLowerCase().includes(query()));
 const proxyTypes = createMemo(() => new Map(nodes().map(p => [p.name, p.type])));
 const fallbackGroup = createMemo(() => groups().find(p => p.name === fallback()?.target));
 const pending = (group: Proxy, name: string) => switching()?.group === group.name && switching()?.name === name;

 return <Workspace page="nodes">
  <section class="intro"><div><span class="eyebrow">PROXY NODES</span><h1>节点管理<span>，点选即切换。</span></h1><p>选择代理组，点击节点卡片即可使用。当前使用的节点会保持高亮。</p></div><button class="secondary" disabled={busy()} onClick={() => void refresh()}>{busy() ? "正在处理…" : "刷新节点与模式"}</button></section>
  <Show when={error()}><p class="error" role="alert">{error()}</p></Show>
  <Show when={notice()}><p class="success" role="status">{notice()}</p></Show>
  <section class="panel mode-panel">
   <div class="panel-heading"><div><h2>内核运行模式</h2><p>当前模式：{modes[mode()] || "未连接"}。切换立即生效。</p></div></div>
   <div class="mode-options"><For each={Object.entries(modes)}>{([value, label]) => <button class={mode() === value ? "" : "secondary"} aria-pressed={mode() === value} disabled={busy() || !ready()} onClick={() => void changeMode(value)}>{label}<small>{({ rule: "按内核规则选择出站", global: "使用 GLOBAL 组选择的节点", direct: "全部流量直接连接" } as Record<string, string>)[value]}</small></button>}</For></div>
   <p>节点和模式切换作用于当前内核；重启或重新加载配置后可能恢复原配置。</p>
   <Show when={ready() && mode() === "rule"}>
    <div class="rule-fallback"><div><h3>未命中规则时的出口</h3><Show when={fallback()} fallback={<p role="status">{fallbackError() || "正在读取兜底规则…"}</p>}>{value => <><p><span class="rule-fallback-route">{value().configured ? "未命中前面的规则时，按 MATCH 规则路由到：" : "未配置有效 MATCH 规则；所有规则均未命中时，内核默认直连："}<strong>{value().target}</strong></span><Show when={fallbackGroup()?.now}><span class="rule-fallback-current">当前选择：{fallbackGroup()?.now}</span></Show></p><small>{fallbackGroup() ? "可进入此代理组选择成员；自动策略由内核选择。" : "此出口由内核规则配置决定。更改兜底目标需修改配置中的 MATCH 规则后重新应用。"}</small></>}</Show></div><Show when={fallbackGroup()}><button class="secondary" disabled={busy()} onClick={() => { setSearch(""); setActiveName(fallbackGroup()!.name); }}>查看兜底代理组</button></Show></div>
   </Show>
  </section>
  <section class="panel node-panel">
   <div class="panel-heading library-heading"><div><h2>代理组与节点</h2><p>规则模式选择对应代理组；全局模式选择 GLOBAL 组。</p></div><Show when={mode() !== "direct"}><label class="search-label">搜索节点 / 代理组<input type="search" value={search()} onInput={e => setSearch(e.currentTarget.value)} placeholder="输入名称，快速找到节点" /></label></Show></div>
   <Show when={ready()} fallback={<div class="empty"><p>{busy() ? "正在读取内核配置…" : "请连接内核后刷新"}</p></div>}>
    <Show when={mode() !== "direct"} fallback={<div class="empty"><p>直连模式</p><small>全部流量直接连接。切换为规则或全局模式后可选择代理节点。</small></div>}>
    <Show when={filtered().length} fallback={<div class="empty">◇<p>{query() ? "没有匹配的节点或代理组" : mode() === "global" ? "当前内核没有可选的 GLOBAL 组" : "当前内核没有可选代理组"}</p><Show when={query()} fallback={<small>请在 Mihomo 加载包含节点与代理组的配置，再刷新此页面。托管内核默认仅提供直连。</small>}><button class="secondary" onClick={() => setSearch("")}>清空搜索</button></Show></div>}>
     <div class="node-workspace">
      <nav class="node-groups" aria-label="代理组" tabindex="0"><div class="node-groups-heading">代理组 <span>{filtered().length}</span></div><For each={filtered()}>{group => <button class="node-group" classList={{ "is-active": activeGroup()?.name === group.name }} aria-pressed={activeGroup()?.name === group.name} onClick={() => setActiveName(group.name)}><span class="node-group-title">{group.name}<span class="node-count">{group.all?.length}</span></span><small>{group.type === "Selector" ? "手动选择" : "自动策略"} · {group.now || "尚无当前节点"}</small></button>}</For></nav>
      <Show when={activeGroup()}>{group => <div class="node-browser" role="region" aria-label="节点列表" tabindex="0" aria-busy={!!switching()}>
       <div class="node-browser-heading"><div><h3>{group().name}</h3><p>{group().type === "Selector" ? "点击下方卡片，立即切换节点" : "由内核自动选择，以下节点仅供查看"}</p></div><span class="badge">{group().type === "Selector" ? "手动选择" : group().type}</span></div>
       <div class="node-current"><span class="node-current-dot" aria-hidden="true"/><div><small>当前使用</small><strong>{group().now || "由内核自动分配"}</strong></div></div>
       <div class="node-list-heading"><span>{query() ? `找到 ${members(group()).length} / ${group().all?.length} 个节点 / 策略` : `${group().all?.length} 个节点 / 策略`}</span><Show when={query()}><button class="node-clear" onClick={() => setSearch("")}>清空搜索</button></Show></div>
       <div class="node-grid"><For each={members(group())}>{name => <Show when={group().type === "Selector"} fallback={<div class="node-tile node-tile-readonly" classList={{ "is-current": group().now === name }}><span class="node-tile-name">{name}</span><span class="node-tile-meta"><span>{proxyTypes().get(name) || "代理"}</span><span>{group().now === name ? "✓ 当前使用" : "自动策略"}</span></span></div>}>
        <button class="node-tile" classList={{ "is-current": group().now === name, "is-switching": pending(group(), name) }} aria-pressed={group().now === name} aria-label={`${name}，${group().now === name ? "当前使用" : "点击切换"}`} disabled={busy()} onClick={() => void choose(group(), name)}><span class="node-tile-name">{name}</span><span class="node-tile-meta"><span>{proxyTypes().get(name) || "代理"}</span><span>{pending(group(), name) ? "切换中…" : group().now === name ? "✓ 当前使用" : "点击使用 →"}</span></span></button>
       </Show>}</For></div>
      </div>}</Show>
     </div>
    </Show>
    </Show>
   </Show>
  </section>
 </Workspace>;
}
