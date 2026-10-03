import { createSignal, For, onMount, Show } from "solid-js";
import Workspace from "../components/Workspace";
import { api } from "../lib/api";
type Proxy = { name: string; type: string; now?: string; all?: string[] };
const modes: Record<string,string> = {rule:"规则模式",global:"全局模式",direct:"直连模式"};
export default function Nodes() {
 const [nodes,setNodes] = createSignal<Proxy[]>([]);
 const [mode,setMode] = createSignal("");
 const [ready,setReady] = createSignal(false);
 const [busy,setBusy] = createSignal(false);
 const [error,setError] = createSignal("");
 const [notice,setNotice] = createSignal("");
 const [search,setSearch] = createSignal("");
 const [selected,setSelected] = createSignal<Record<string,string>>({});
 async function refresh() {
  setBusy(true);setError("");
  try { const [list,runtime] = await Promise.all([api<Proxy[]>("nodes"),api<{mode:string}>("runtime/mode")]);setNodes(list);setMode(runtime.mode);setSelected(Object.fromEntries(list.map(p=>[p.name,p.now||""])));setReady(true); }
  catch(e) {setReady(false);setError((e as Error).message);} finally {setBusy(false);}
 }
 onMount(()=>void refresh());
 async function changeMode(value: string) {
  setBusy(true);setNotice("");setError("");
  try {const result=await api<{mode:string}>("runtime/mode",{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({mode:value})});setMode(result.mode);setNotice(`内核已切换为${modes[result.mode]}。`);}
  catch(e) {setError((e as Error).message);} finally {setBusy(false);}
 }
 async function choose(group: Proxy) {
  const name=selected()[group.name];setBusy(true);setError("");setNotice("");
  try {await api("nodes/selection",{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({group:group.name,name})});setNodes(nodes().map(p=>p.name===group.name?{...p,now:name}:p));setNotice(`「${group.name}」已选择「${name}」。`);}
  catch(e) {setError((e as Error).message);} finally {setBusy(false);}
 }
 const groups=()=>nodes().filter(p=>p.all?.length);
 const filtered=()=>groups().filter(p=>p.name.toLowerCase().includes(search().toLowerCase())||p.all?.some(n=>n.toLowerCase().includes(search().toLowerCase())));
 return <Workspace page="nodes"><section class="intro"><div><span class="eyebrow">PROXY NODES</span><h1>节点管理<span>，自由选择。</span></h1><p>读取当前内核配置的节点与代理组，切换运行模式和手动代理组的节点。</p></div><button class="secondary" disabled={busy()} onClick={()=>void refresh()}>{busy()?"正在处理…":"刷新节点与模式"}</button></section><Show when={error()}><p class="error" role="alert">{error()}</p></Show><Show when={notice()}><p class="success" role="status">{notice()}</p></Show><section class="panel mode-panel"><div class="panel-heading"><div><h2>内核运行模式</h2><p>当前模式：{modes[mode()]||"未连接"}。切换立即生效；配置库和快速配置页的草稿仍可单独保存。</p></div></div><div class="mode-options"><For each={Object.entries(modes)}>{([value,label])=><button class={mode()===value?"":"secondary"} aria-pressed={mode()===value} disabled={busy()||!ready()} onClick={()=>void changeMode(value)}>{label}<small>{({rule:"按内核规则选择出站",global:"使用 GLOBAL 组选择的节点",direct:"全部流量直接连接"} as Record<string,string>)[value]}</small></button>}</For></div><p>节点选择与模式切换作用于当前内核；内核重启或重新加载配置后可能恢复原配置。</p></section><section class="panel"><div class="panel-heading library-heading"><div><h2>代理组与节点</h2><p>规则模式使用规则指定的代理组；全局模式请在 GLOBAL 组选择节点。自动策略组只展示当前结果。</p></div><label class="search-label">搜索节点 / 代理组<input type="search" value={search()} onInput={e=>setSearch(e.currentTarget.value)} placeholder="输入节点或代理组名称"/></label></div><Show when={ready()} fallback={<div class="empty"><p>{busy()?"正在读取内核配置…":"请连接内核后刷新"}</p></div>}><Show when={filtered().length} fallback={<div class="empty">◇<p>{search()?"没有匹配的节点或代理组":"当前内核没有可选代理组"}</p><small>请在 Mihomo 加载包含节点与代理组的配置，再刷新此页面。托管内核默认仅提供直连。</small></div>}><div class="profile-grid"><For each={filtered()}>{group=><article class="profile-card"><div class="profile-top"><h3>{group.name}</h3><span class="badge">{group.type}</span></div><p>当前选择：{group.now||"由内核自动分配"}</p><Show when={group.type==="Selector"} fallback={<p>此代理组由内核自动选择。</p>}><label class="form-label">选择节点<select disabled={busy()} value={selected()[group.name]||""} onChange={e=>setSelected({...selected(),[group.name]:e.currentTarget.value})}><option value="" disabled>请选择节点</option><For each={group.all}>{name=><option value={name}>{name}</option>}</For></select></label><button disabled={busy()||!selected()[group.name]||selected()[group.name]===group.now} onClick={()=>void choose(group)}>使用所选节点</button></Show><details class="node-members"><summary>{group.all?.length} 个节点 / 策略</summary><For each={group.all}>{name=><div class="node-member"><span>{name}</span><small>{nodes().find(p=>p.name===name)?.type||"代理"}</small></div>}</For></details></article>}</For></div></Show></Show></section></Workspace>;
}
