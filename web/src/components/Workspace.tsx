import { useI18n } from "../lib/i18n";
import { createEffect, createSignal, onCleanup, onMount, type JSX } from "solid-js";
import AppUpdates from "./AppUpdates";
import { api } from "../lib/api";

function SidebarIcon(props: { children: JSX.Element; brand?: boolean }) {
 return <svg class={props.brand ? "brand-icon" : "nav-icon"} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">{props.children}</svg>;
}

export default function Workspace(props: { page: "quick" | "profiles" | "tasks" | "nodes" | "core"; online?: boolean; children: JSX.Element }) {
 const { t, locale, setLocale } = useI18n();
 const [online, setOnline] = createSignal(false);
 createEffect(() => { if (typeof document !== "undefined") document.title = t("Nulas · {p0}", { p0: ({quick:t("快速配置"),profiles:t("配置管理"),tasks:t("后台任务"),nodes:t("节点管理"),core:t("内核管理")})[props.page] }); });
 onMount(() => {
  const refresh = async () => { try { setOnline((await api<{ status: string }>("health")).status === "ok"); } catch { setOnline(false); } };
  void refresh(); const timer = setInterval(() => void refresh(), 5000); onCleanup(() => clearInterval(timer));
 });
 return <div class="shell"><aside>
  <a class="brand" href="/"><SidebarIcon brand><path d="m12 2 10 10-10 10L2 12Z"/><path d="m12 7 5 5-5 5-5-5Z"/></SidebarIcon><span>Nulas<small>CORE CONFIGURATION</small></span></a>
  <a class="nav" classList={{active:props.page==="quick"}} href="/"><SidebarIcon><rect x="3" y="3" width="18" height="18" rx="3"/><path d="M9 3v18M9 10h12"/></SidebarIcon><span>{t("快速配置")}</span></a>
  <a class="nav" classList={{active:props.page==="profiles"}} href="/?view=profiles"><SidebarIcon><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9Z"/><path d="M14 3v6h6M8 13h8M8 17h8"/></SidebarIcon><span>{t("配置管理")}</span></a>
  <a class="nav" classList={{active:props.page==="nodes"}} href="/nodes"><SidebarIcon><circle cx="12" cy="5" r="2"/><circle cx="5" cy="19" r="2"/><circle cx="19" cy="19" r="2"/><path d="M12 7v5M5 17v-5h14v5"/></SidebarIcon><span>{t("节点管理")}</span></a>
  <a class="nav" classList={{active:props.page==="core"}} href="/core"><SidebarIcon><rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9" y="9" width="6" height="6" rx="1"/><path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3"/></SidebarIcon><span>{t("内核管理")}</span></a>
  <a class="nav" classList={{active:props.page==="tasks"}} href="/tasks"><SidebarIcon><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></SidebarIcon><span>{t("后台任务")}</span></a>
  <label class="language-select">{t("界面语言")}<select aria-label={t("界面语言")} value={locale()} onChange={e => setLocale(e.currentTarget.value === "en" ? "en" : "zh-CN")}><option value="zh-CN">简体中文</option><option value="en">English</option></select></label>
  <AppUpdates />
  <div class="sidebar-note">{t("为更快的配置而构建")}<p>SolidStart + Go<br/>Mihomo compatible</p></div>
 </aside><main><header><span>{t("工作空间 /")} {({quick:t("概览"),profiles:t("配置管理"),nodes:t("节点管理"),tasks:t("后台任务"),core:t("内核管理")})[props.page]}</span><span class="connection" classList={{online:props.online ?? online()}}>● {(props.online ?? online())?t("后端已连接"):t("后端未连接")}</span></header>{props.children}<footer>
  <div class="footer-project">{t("NULAS / 本地配置工作空间")}<div class="footer-meta">
    <a href="https://github.com/zilorn/nulas" target="_blank" rel="noopener noreferrer" aria-label={t("Nulas GitHub 仓库（在新标签页打开）")}>GitHub ↗</a>
    <span title={__NULAS_BUILD_HASH__ || undefined}>{__NULAS_BUILD_HASH__ ? <>{t("构建版本")} <code>{__NULAS_BUILD_HASH__.slice(0, 7)}</code></> : t("构建版本未知")}</span>
   </div>
  </div>
  <span>{t("支持完整配置导入和节点选择，自启管理支持 Linux，系统代理仅支持 GNOME 桌面。")}</span>
 </footer></main></div>;
}
