import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import Workspace from "../components/Workspace";
import { api, type Job } from "../lib/api";
const labels: Record<string,string> = { queued:"等待执行", running:"执行中", succeeded:"已完成", failed:"失败" };
export default function Tasks() {
 const [jobs,setJobs] = createSignal<Job[]>([]);
 const [loading,setLoading] = createSignal(true);
 const [error,setError] = createSignal("");
 const [filter,setFilter] = createSignal("all");
 let refreshing = false;
 const refresh = async () => { if(refreshing) return; refreshing=true; try { setJobs(await api<Job[]>("jobs")); setError(""); } catch(e) { setError((e as Error).message); } finally { setLoading(false); refreshing=false; } };
 onMount(() => { void refresh();const timer=setInterval(()=>void refresh(),2000);onCleanup(()=>clearInterval(timer)); });
 const count = (statuses: string[]) => jobs().filter(j=>statuses.includes(j.status)).length;
 const visible = () => [...jobs()].reverse().filter(j=>filter()==="all"||j.status===filter());
 return <Workspace page="tasks"><section class="intro"><div><span class="eyebrow">BACKGROUND TASKS</span><h1>后台任务<span>，持续运行。</span></h1><p>关闭页面后任务仍会执行。执行结果与配置快照由后台保存。</p></div></section><div class="stats"><article><span>等待 / 执行中</span><strong>{count(["queued","running"])}</strong><small>后台独立执行</small></article><article><span>已完成</span><strong>{count(["succeeded"])}</strong><small>操作成功</small></article><article><span>失败</span><strong>{count(["failed"])}</strong><small>查看结果后可返回原页面重试</small></article></div><Show when={error()}><p role="alert" class="error">{error()} · 列表可能不是最新状态</p></Show><section class="panel"><div class="panel-heading"><div><h2>任务记录</h2><p>每 2 秒自动更新，最新任务优先显示。</p></div><label class="search-label">筛选状态<select value={filter()} onChange={e=>setFilter(e.currentTarget.value)}><option value="all">全部状态</option><For each={Object.entries(labels)}>{([value,label])=><option value={value}>{label}</option>}</For></select></label></div><Show when={!loading()} fallback={<p role="status">正在加载任务…</p>}><Show when={visible().length} fallback={<div class="empty">◷<p>暂无{filter()==="all"?"":"匹配的"}任务</p><small><a href="/">返回快速配置提交任务 ↗</a></small></div>}><div class="table-wrap"><table><thead><tr><th>操作</th><th>状态</th><th>结果</th><th>提交时间</th></tr></thead><tbody><For each={visible()}>{j=><tr><td>{j.action==="proxy-enable"?"开启系统代理":j.action==="proxy-disable"?"恢复系统代理":j.action==="tun-enable"?"开启 TUN":j.action==="tun-disable"?"关闭 TUN":j.action==="install-core"?"安装 / 启动 Mihomo":j.action==="apply"?"应用运行配置":"生成配置文件"}<small class="job-id">{j.id.slice(0,8)}</small></td><td><span class={`status ${j.status}`}>{labels[j.status]||j.status}</span></td><td>{j.message}</td><td>{new Date(j.created).toLocaleString("zh-CN")}</td></tr>}</For></tbody></table></div></Show></Show></section></Workspace>;
}
