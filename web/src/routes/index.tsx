import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { api, type Config, type Job } from "../lib/api";
import SystemSettings from "../components/SystemSettings";
import Workspace from "../components/Workspace";
import ConfigurationManager from "../components/ConfigurationManager";
export default function Home() {
 const [searchParams, setSearchParams] = useSearchParams();
 const view = () => searchParams.view === "profiles" ? "profiles" : "quick";
 const [config,setConfig] = createSignal<Config>({"mixed-port":7890,mode:"rule","allow-lan":false,ipv6:false,"log-level":"info"});
 const [jobs,setJobs] = createSignal<Job[]>([]);
 const [core,setCore] = createSignal({status:"checking",message:"正在检查内核…"});
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
  try { await api("config",{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify(config())});
   if(action){await api("jobs",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({action})});setNotice("后台任务已提交，可在后台任务页查看结果，关闭网页后仍会继续执行。");}else setNotice("配置已保存。");await refresh();
  }catch(e){setError((e as Error).message);}finally{setBusy(false);}
 }
 return <Workspace page={view()} online={online()}>
  <Show when={view()==="profiles"}><ConfigurationManager onLoad={c=>{setConfig(c);setReady(true);setSearchParams({ view: undefined });setError("");setNotice("配置已载入，可继续编辑；应用到内核需单独提交任务。");}} /></Show>
  <Show when={view()==="quick"}>
   <section class="intro config-intro"><div><h1>快速配置</h1><p>调整常用参数，保存或应用到内核。</p></div><a class="config-link" href="/?view=profiles">打开配置库 ↗</a></section>
   <div class="config-layout">
    <section class="panel quick-panel" aria-label="核心配置">
     <div class="panel-heading"><div><h2>核心配置</h2><p>这里显示的是编辑内容，应用后才会生效。</p></div><span class="badge">MIHOMO</span></div>
     <form onSubmit={e=>{e.preventDefault();void save();}}><fieldset disabled={!ready()||busy()||pending()}>
      <div class="config-section"><h3>代理模式</h3><div class="config-modes" role="group" aria-label="代理模式">
       <For each={[{value:"rule",name:"规则",description:"按规则分流"},{value:"global",name:"全局",description:"全部通过代理"},{value:"direct",name:"直连",description:"全部直接连接"}]}>{mode=><button type="button" class="config-mode" classList={{"is-selected":config().mode===mode.value}} aria-pressed={config().mode===mode.value} onClick={()=>update("mode",mode.value)}><span>{mode.name}</span><small>{mode.description}</small></button>}</For>
      </div></div>
      <div class="config-section"><h3>连接设置</h3><div class="config-fields"><label>混合代理端口<input type="number" min="1" max="65535" required value={config()["mixed-port"]} onInput={e=>update("mixed-port",e.currentTarget.valueAsNumber)}/><small>HTTP / SOCKS5 共用端口</small></label><label>日志级别<select value={config()["log-level"]} onChange={e=>update("log-level",e.currentTarget.value)}><For each={[{value:"info",label:"信息 · info"},{value:"warning",label:"警告 · warning"},{value:"error",label:"错误 · error"},{value:"debug",label:"调试 · debug"},{value:"silent",label:"静默 · silent"}]}>{level=><option value={level.value}>{level.label}</option>}</For></select></label></div>
       <label class="toggle"><span>允许局域网连接<small>让同一网络中的设备使用代理</small></span><input type="checkbox" checked={config()["allow-lan"]} onChange={e=>update("allow-lan",e.currentTarget.checked)}/></label>
       <label class="toggle"><span>IPv6 支持<small>启用 IPv6 网络连接</small></span><input type="checkbox" checked={config().ipv6} onChange={e=>update("ipv6",e.currentTarget.checked)}/></label>
      </div>
      <div class="config-save"><p>保存仅记录参数；生成和应用由后台执行。</p><div class="actions"><button type="submit" class="secondary">保存配置</button><button type="button" class="secondary" onClick={e=>{if(e.currentTarget.form?.reportValidity())void save("generate");}}>生成文件</button><button type="button" disabled={!controller()} onClick={e=>{if(e.currentTarget.form?.reportValidity())void save("apply");}}>{busy()?"正在提交…":"保存并应用"}</button></div><Show when={!controller()}><p>内核尚未连接，连接后可应用配置。</p></Show></div>
     </fieldset></form>
     <Show when={notice()}><p role="status" class="success config-feedback">{notice()}</p></Show><Show when={error()}><p role="alert" class="error config-feedback">{error()}</p></Show>
    </section>
    <div class="config-side">
     <section class="panel runtime-panel" aria-label="运行状态"><div class="panel-heading"><h2>运行状态</h2><a href="/tasks">查看任务 ↗</a></div><div class="runtime-state" role="status"><span class="runtime-dot" classList={{"is-running":core().status==="running","is-failed":core().status==="failed"}}/><strong>{({checking:"正在检查内核",missing:"内核未安装",installing:"正在准备内核",starting:"正在启动内核",running:"内核运行中",installed:"内核已安装",failed:"内核安装失败"} as Record<string,string>)[core().status]}</strong></div><p>{pending()?"有任务等待或执行中，请稍候。":"后台任务空闲"}</p><details class="config-details"><summary>内核详情</summary><p>{core().message}</p><p>{controller()?"已配置 Mihomo 控制接口。":"等待内核连接；已有内核可在后端设置 MIHOMO_CONTROLLER。"}</p></details><Show when={core().status==="failed"}><button type="button" class="secondary" disabled={busy()||pending()} onClick={()=>void retryCore()}>重试安装 / 启动</button></Show></section>
     <SystemSettings />
     <p class="config-background-note">关闭网页后，已提交的任务仍会继续。<a href="/tasks">查看执行结果 ↗</a></p>
    </div>
   </div>
  </Show>
 </Workspace>;
}
