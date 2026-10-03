import { createEffect, createSignal, onCleanup, onMount, type JSX } from "solid-js";
import { api } from "../lib/api";
export default function Workspace(props: { page: "quick" | "profiles" | "tasks" | "nodes" | "core"; online?: boolean; children: JSX.Element }) {
 const [online, setOnline] = createSignal(false);
 createEffect(() => { if (typeof document !== "undefined") document.title = `Nulas · ${({quick:"快速配置",profiles:"配置管理",tasks:"后台任务",nodes:"节点管理",core:"内核管理"})[props.page]}`; });
 onMount(() => {
  const refresh = async () => { try { setOnline((await api<{ status: string }>("health")).status === "ok"); } catch { setOnline(false); } };
  void refresh(); const timer = setInterval(() => void refresh(), 5000); onCleanup(() => clearInterval(timer));
 });
 return <div class="shell"><aside><a class="brand" href="/">◈ <span>Nulas<small>CORE CONFIGURATION</small></span></a><a class="nav" classList={{active:props.page==="quick"}} href="/">◫　快速配置</a><a class="nav" classList={{active:props.page==="profiles"}} href="/?view=profiles">▤　配置管理</a><a class="nav" classList={{active:props.page==="nodes"}} href="/nodes">◇　节点管理</a><a class="nav" classList={{active:props.page==="core"}} href="/core">⬡　内核管理</a><a class="nav" classList={{active:props.page==="tasks"}} href="/tasks">↻　后台任务</a><div class="sidebar-note">为更快的配置而构建<p>SolidStart + Go<br/>Mihomo compatible</p></div></aside><main><header><span>工作空间 / {({quick:"概览",profiles:"配置管理",nodes:"节点管理",tasks:"后台任务",core:"内核管理"})[props.page]}</span><span class="connection" classList={{online:props.online ?? online()}}>● {(props.online ?? online())?"后端已连接":"后端未连接"}</span></header>{props.children}<footer>NULAS / 本地配置工作空间<span>支持完整配置导入和节点选择，自启管理支持 Linux，系统代理仅支持 GNOME 桌面。</span></footer></main></div>;
}
