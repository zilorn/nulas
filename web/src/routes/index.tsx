import { useI18n } from "../lib/i18n";
import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { api, type Config, type Job } from "../lib/api";
import SystemSettings from "../components/SystemSettings";
import Workspace from "../components/Workspace";
import ConfigurationManager from "../components/ConfigurationManager";
export default function Home() {
 const { t, locale } = useI18n();
 const [searchParams, setSearchParams] = useSearchParams();
 const view = () => searchParams.view === "profiles" ? "profiles" : "quick";
 const [config,setConfig] = createSignal<Config>({"mixed-port":7890,mode:"rule","allow-lan":false,ipv6:false,"log-level":"info"});
 const [jobs,setJobs] = createSignal<Job[]>([]);
 const [core,setCore] = createSignal({status:"checking",message:t("正在检查内核…")});
 const [ready,setReady] = createSignal(false);
 const [controller,setController] = createSignal(false);
 const [busy,setBusy] = createSignal(false);
 const [notice,setNotice] = createSignal("");
 const [error,setError] = createSignal("");
 const [online,setOnline] = createSignal(false);
 const pending = () => jobs().some(j => j.status === "queued" || j.status === "running");
 const refresh = async () => { try { const [list,health,status] = await Promise.all([api<Job[]>("jobs"),api<{controllerConfigured:boolean}>("health"),api<{status:string;message:string}>("core")]);setCore(status);setJobs(list);setController(health.controllerConfigured);setOnline(true); } catch { setOnline(false); } };
 onMount(() => { void (async()=>{try{setConfig(await api<Config>("config"));setReady(true);}catch(e){setError((e as Error).message);}try { setCore(await api<{status:string;message:string}>("core/ensure",{method:"POST",headers:{"Content-Type":"application/json"},body:"{}"})); } catch(e) { setCore({status:"failed",message:(e as Error).message});setError((e as Error).message); } await refresh();})();const timer=setInterval(()=>void refresh(),2000);onCleanup(()=>clearInterval(timer)); });
 async function retryCore() { setBusy(true);setError("");try { await api("jobs",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({action:"install-core"})});await refresh(); } catch(e) {setError((e as Error).message);} finally {setBusy(false);} }
 const update = <K extends keyof Config>(key: K,value: Config[K]) => setConfig({...config(),[key]:value});
 async function save(action?:string) {
  setBusy(true);setError("");setNotice("");
  try {
   try {
    await api("config",{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify(config())});
   } catch(e) { setError((e as Error).message);return; }
   setNotice(t("配置已保存。"));
   if(action) {
    try {
     await api("jobs",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({action})});
     setNotice(t("配置已保存。后台任务已提交，可在后台任务页查看结果，关闭网页后仍会继续执行。"));
    } catch(e) { setError(t("后台任务提交失败：{message}",{message:(e as Error).message})); }
   }
   await refresh();
  } finally { setBusy(false); }
 }
 return <Workspace page={view()} online={online()}>
  <Show when={view()==="profiles"}><ConfigurationManager onLoad={c=>{setConfig(c);setReady(true);setSearchParams({ view: undefined });setError("");setNotice(t("配置已载入，可继续编辑；应用到内核需单独提交任务。"));}} /></Show>
  <Show when={view()==="quick"}>
   <section class="intro config-intro"><div><h1>{t("快速配置")}</h1><p>{t("调整常用参数，保存或应用到内核。")}</p></div><a class="config-link" href="/?view=profiles">{t("打开配置库 ↗")}</a></section>
   <div class="config-layout">
    <section class="panel quick-panel" aria-label={t("核心配置")}>
     <div class="panel-heading"><div><h2>{t("核心配置")}</h2><p>{t("这里显示的是编辑内容，应用后才会生效。")}</p></div><span class="badge">MIHOMO</span></div>
     <form onSubmit={e=>{e.preventDefault();void save();}}><fieldset disabled={!ready()||busy()||pending()}>
      <div class="config-section"><h3>{t("代理模式")}</h3><div class="config-modes" role="group" aria-label={t("代理模式")}>
       <For each={[{value:"rule",name:t("规则"),description:t("按规则分流")},{value:"global",name:t("全局"),description:t("全部通过代理")},{value:"direct",name:t("直连"),description:t("全部直接连接")}]}>{mode=><button type="button" class="config-mode" classList={{"is-selected":config().mode===mode.value}} aria-pressed={config().mode===mode.value} onClick={()=>update("mode",mode.value)}><span>{mode.name}</span><small>{mode.description}</small></button>}</For>
      </div></div>
      <div class="config-section"><h3>{t("连接设置")}</h3><div class="config-fields"><label>{t("混合代理端口")}<input type="number" min="1" max="65535" required value={config()["mixed-port"]} onInput={e=>update("mixed-port",e.currentTarget.valueAsNumber)}/><small>{t("HTTP / SOCKS5 共用端口")}</small></label><label>{t("日志级别")}<select value={config()["log-level"]} onChange={e=>update("log-level",e.currentTarget.value)}><For each={[{value:"info",label:t("信息 · info")},{value:"warning",label:t("警告 · warning")},{value:"error",label:t("错误 · error")},{value:"debug",label:t("调试 · debug")},{value:"silent",label:t("静默 · silent")}]}>{level=><option value={level.value}>{level.label}</option>}</For></select></label></div>
       <label class="toggle"><span>{t("允许局域网连接")}<small>{t("让同一网络中的设备使用代理")}</small></span><input type="checkbox" checked={config()["allow-lan"]} onChange={e=>update("allow-lan",e.currentTarget.checked)}/></label>
       <label class="toggle"><span>{t("IPv6 支持")}<small>{t("启用 IPv6 网络连接")}</small></span><input type="checkbox" checked={config().ipv6} onChange={e=>update("ipv6",e.currentTarget.checked)}/></label>
      </div>
      <div class="config-save"><p>{t("保存仅记录参数；生成和应用由后台执行。")}</p><div class="actions"><button type="submit" class="secondary">{t("保存配置")}</button><button type="button" class="secondary" onClick={e=>{if(e.currentTarget.form?.reportValidity())void save("generate");}}>{t("生成文件")}</button><button type="button" disabled={!controller()} onClick={e=>{if(e.currentTarget.form?.reportValidity())void save("apply");}}>{busy()?t("正在提交…"):t("保存并应用")}</button></div><Show when={!controller()}><p>{t("内核尚未连接，连接后可应用配置。")}</p></Show></div>
     </fieldset></form>
     <Show when={notice()}><p role="status" class="success config-feedback">{notice()}</p></Show><Show when={error()}><p role="alert" class="error config-feedback">{error()}</p></Show>
    </section>
    <div class="config-side">
     <section class="panel runtime-panel" aria-label={t("运行状态")}><div class="panel-heading"><h2>{t("运行状态")}</h2><a href="/tasks">{t("查看任务 ↗")}</a></div><div class="runtime-state" role="status"><span class="runtime-dot" classList={{"is-running":core().status==="running","is-failed":core().status==="failed"}}/><strong>{({checking:t("正在检查内核"),missing:t("内核未安装"),installing:t("正在准备内核"),starting:t("正在启动内核"),running:t("内核运行中"),installed:t("内核已安装"),failed:t("内核安装失败")} as Record<string,string>)[core().status]}</strong></div><p>{pending()?t("有任务等待或执行中，请稍候。"):t("后台任务空闲")}</p><details class="config-details"><summary>{t("内核详情")}</summary><p>{core().message}</p><p>{controller()?t("已配置 Mihomo 控制接口。"):t("等待内核连接；已有内核可在后端设置 MIHOMO_CONTROLLER。")}</p></details><Show when={core().status==="failed"}><button type="button" class="secondary" disabled={busy()||pending()} onClick={()=>void retryCore()}>{t("重试安装 / 启动")}</button></Show></section>
     <SystemSettings />
     <p class="config-background-note">{t("关闭网页后，已提交的任务仍会继续。")}<a href="/tasks">{t("查看执行结果 ↗")}</a></p>
    </div>
   </div>
  </Show>
 </Workspace>;
}
