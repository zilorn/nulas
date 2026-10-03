import { useI18n } from "../lib/i18n";
import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { api, type Job } from "../lib/api";

type SystemFeature = { supported: boolean; enabled: boolean; recovery?: boolean; message: string };
type SettingsStatus = { proxy: SystemFeature; startup: SystemFeature };
type TUNStatus = { supported: boolean; ready: boolean; enabled: boolean; message: string; commands: string[] };
type TrayStatus = { supported: boolean; enabled: boolean; running: boolean; message: string };
type Preferences = { tray: boolean; proxy?: boolean; tun?: boolean; startup?: boolean };
export default function SystemSettings() {
 const { t, locale } = useI18n();
 const [tray, setTray] = createSignal<TrayStatus>();
 const [preferences, setPreferences] = createSignal<Preferences>();
 const [trayDetails, setTrayDetails] = createSignal(false);
 const [system, setSystem] = createSignal<SettingsStatus>();
 const [proxyDetails, setProxyDetails] = createSignal(false);
 const [startupDetails, setStartupDetails] = createSignal(false);
 const [status, setStatus] = createSignal<TUNStatus>();
 const [error, setError] = createSignal("");
 const [notice, setNotice] = createSignal("");
 const [busy, setBusy] = createSignal(false);
 const [pending, setPending] = createSignal(false);
 const [details, setDetails] = createSignal(false);
 let refreshing = false;
 const refresh = async () => {
  if (refreshing) return;
  refreshing = true;
  try {
   const [tun, jobs, settings, trayStatus, saved] = await Promise.allSettled([api<TUNStatus>("runtime/tun"), api<Job[]>("jobs"), api<SettingsStatus>("runtime/system"), api<TrayStatus>("runtime/tray"), api<Preferences>("preferences")]);
   setTray(trayStatus.status === "fulfilled" ? trayStatus.value : undefined);
   setPreferences(saved.status === "fulfilled" ? saved.value : undefined);
   setStatus(tun.status === "fulfilled" ? tun.value : undefined);
   setSystem(settings.status === "fulfilled" ? settings.value : undefined);
   setPending(jobs.status !== "fulfilled" || jobs.value.some(j => j.status === "queued" || j.status === "running"));
   const failure = [tun, jobs, settings, trayStatus, saved].find(result => result.status === "rejected");
   setError(failure?.status === "rejected" ? String(failure.reason.message || failure.reason) : "");
  } catch (e) { setError((e as Error).message); } finally { refreshing = false; }
 };
 onMount(() => { void refresh(); const timer = setInterval(() => void refresh(), 5000); onCleanup(() => clearInterval(timer)); });
 async function toggle(enable: boolean) {
  setBusy(true); setNotice("");
  try {
   // Always recheck after the user's terminal operation, then enqueue the durable job.
   if (enable) {
    const checked = await api<TUNStatus>("runtime/tun"); setStatus(checked);
    if (!checked.supported || !checked.ready) { setNotice(checked.message); setDetails(true); return; }
   }
   await api("jobs", { method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({action: enable ? "tun-enable" : "tun-disable"}) });
   setPending(true); setNotice(t("TUN 任务已提交。请在后台任务页查看检查结果，关闭网页后任务仍会执行。"));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function changeSystem(key: "startup") {
  setBusy(true); setError(""); setNotice("");
  try { setSystem(await api<SettingsStatus>("runtime/system", { method: "PUT", headers: {"Content-Type":"application/json"}, body: JSON.stringify({[key]: !(system()?.startup.enabled || system()?.startup.recovery)}) })); setNotice(t("设置已保存；启动选项在下次自启时生效。")); }
  catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function toggleProxy() {
  setBusy(true); setError(""); setNotice("");
  try { await api("jobs", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({action:system()?.proxy.recovery ? "proxy-disable" : "proxy-enable"}) }); setPending(true); setNotice(t("系统代理任务已提交，请在后台任务页查看结果。")); }
  catch(e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function toggleTray(enable: boolean) {
  setBusy(true); setError(""); setNotice("");
  try {
   const result = await api<TrayStatus>("runtime/tray", { method:"PUT", headers:{"Content-Type":"application/json"}, body:JSON.stringify({enabled:enable}) });
   setTray(result); setTrayDetails(true);
   setNotice(result.enabled && !result.running ? result.message : t("托盘偏好已保存。"));
  } catch(e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 const preferenceNote = (key: "proxy" | "tun" | "startup", actual?: boolean) => {
  const saved = preferences()?.[key];
  return saved === undefined ? "" : t("已保存偏好：{p0}。{p1}", { p0: saved ? t("开启") : t("关闭"), p1: saved !== actual ? t("实际状态与偏好不同；请查看后台任务的恢复进度或失败原因。") : "" });
 };
 return <section class="system-settings" aria-label={t("系统设置")}>
  <h3>{t("系统设置")}</h3>
  <div class="system-row" classList={{"is-enabled":tray()?.running}}><span class="system-label">{t("系统托盘")}</span><button type="button" class="system-icon" aria-label={t("托盘说明")} aria-expanded={trayDetails()} onClick={() => setTrayDetails(!trayDetails())}>ⓘ</button><button type="button" role="switch" aria-label={t("系统托盘")} aria-checked={tray()?.enabled ?? false} class="system-switch" disabled={busy() || !tray()?.supported} onClick={() => void toggleTray(!tray()?.enabled)}><span /></button></div>
  <Show when={trayDetails() || (tray()?.enabled && !tray()?.running)}><div class="system-details"><p role="status">{tray()?.running ? t("托盘已启动") : t("托盘未运行")} · {tray()?.message}</p><p>{t("偏好保存在后端，重启时恢复。托盘位于后端所在的桌面会话；关闭网页或托盘不停止后台任务。")}</p><Show when={tray()?.enabled && !tray()?.running}><button type="button" class="secondary" disabled={busy()} onClick={() => void toggleTray(true)}>{t("重试启动托盘")}</button></Show></div></Show>
  <div class="system-row" classList={{"is-enabled":status()?.enabled}}>
   <span class="system-state" classList={{"is-enabled":status()?.enabled}} aria-hidden="true">{status()?.enabled ? "⊙" : "⊜"}</span><span class="system-label">{t("虚拟网卡模式")}</span>
   <button class="system-icon" type="button" aria-label={t("虚拟网卡设置")} aria-expanded={details()} onClick={() => setDetails(!details())}>⚙</button>
   <button type="button" role="switch" aria-label={t("虚拟网卡模式")} aria-checked={status()?.enabled ?? false} class="system-switch" disabled={busy() || pending() || !status()?.supported} onClick={() => void toggle(!status()?.enabled)}><span /></button>
  </div>
  <Show when={details()}><div class="system-details">
   <p>{preferenceNote("tun", status()?.enabled)}</p><p role="status">{status()?.enabled ? t("已开启") : status() ? t("已关闭") : t("状态未知")} · {status()?.message || t("正在检查内核状态…")}</p><p>{t("开启后接管本机网络路由。初始配置仅直连，节点与规则沿用当前内核配置，不劫持 DNS；服务重启后会在启动任务完成后自动恢复已保存的开启偏好；权限不足或上次操作失败、中断时需手动重试。")}</p>
   <Show when={status()?.commands.length}><p>{t("在后端所在主机的终端执行（需要管理员密码）：")}</p><For each={status()?.commands}>{command => <pre><code>{command}</code></pre>}</For><p>{t("授权后停止并重新启动原服务，再返回开启。容器还需提供网络权限与 /dev/net/tun；内核升级后需重新授权。")}</p></Show>
   <button type="button" class="secondary" disabled={busy()} onClick={() => void refresh()}>{t("重新检查")}</button> <a href="/tasks">{t("查看任务结果 ↗")}</a>
  </div></Show>
  <div class="system-row"><span class="system-state" classList={{"is-enabled":system()?.proxy.enabled}} aria-hidden="true">{system()?.proxy.enabled ? "⊙" : "⊜"}</span><span class="system-label">{t("系统代理")}</span><button type="button" class="system-icon" aria-label={t("系统代理设置")} aria-expanded={proxyDetails()} onClick={() => setProxyDetails(!proxyDetails())}>⚙</button><button type="button" role="switch" aria-label={t("系统代理")} aria-checked={system()?.proxy.enabled ?? false} class="system-switch" disabled={busy() || pending() || !system()?.proxy.supported} onClick={() => void toggleProxy()}><span /></button></div>
  <Show when={proxyDetails() || (system() && (!system()?.proxy.supported || system()?.proxy.recovery && !system()?.proxy.enabled))}><div class="system-details"><p>{system()?.proxy.message}</p><p>{preferenceNote("proxy", system()?.proxy.enabled)}</p><Show when={system()?.proxy.recovery && !system()?.proxy.enabled}><button type="button" class="secondary" disabled={busy() || pending()} onClick={() => void toggleProxy()}>{t("恢复原代理设置")}</button></Show></div></Show>
  <div class="system-row"><span class="system-label">{t("开机自启")}</span><button type="button" class="system-icon" aria-label={t("开机自启说明")} aria-expanded={startupDetails()} onClick={() => setStartupDetails(!startupDetails())}>ⓘ</button><button type="button" role="switch" aria-label={t("开机自启")} aria-checked={system()?.startup.enabled ?? false} class="system-switch" disabled={busy() || !system() || (!system()?.startup.supported && !system()?.startup.recovery)} onClick={() => void changeSystem("startup")}><span /></button></div>
  <Show when={startupDetails() || (system() && !system()?.startup.supported)}><div class="system-details"><p>{system()?.startup.message}</p><p>{preferenceNote("startup", system()?.startup.enabled)}</p></div></Show>
  <p>{t("系统代理、虚拟网卡与自启偏好随操作保存；重启后自动恢复已保存的网络开启偏好，结果见后台任务；开关显示实际状态。失败或中断的操作需手动重试。")}</p>
  <Show when={notice()}><p role="status">{notice()}</p></Show><Show when={error()}><p role="alert" class="error">{error()}</p></Show>
 </section>;
}
