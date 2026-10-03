import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { api, type Job } from "../lib/api";

type TUNStatus = { supported: boolean; ready: boolean; enabled: boolean; message: string; commands: string[] };
export default function TUNControl() {
 const [status, setStatus] = createSignal<TUNStatus>();
 const [error, setError] = createSignal("");
 const [notice, setNotice] = createSignal("");
 const [busy, setBusy] = createSignal(false);
 const [pending, setPending] = createSignal(false);
 const refresh = async () => {
  try {
   const [state, jobs] = await Promise.all([api<TUNStatus>("runtime/tun"), api<Job[]>("jobs")]);
   setStatus(state); setPending(jobs.some(j => j.status === "queued" || j.status === "running")); setError("");
  } catch (e) { setStatus(undefined); setError((e as Error).message); }
 };
 onMount(() => { void refresh(); const timer = setInterval(() => void refresh(), 5000); onCleanup(() => clearInterval(timer)); });
 async function toggle(enable: boolean) {
  setBusy(true); setNotice("");
  try {
   // Always recheck after the user's terminal operation, then enqueue the durable job.
   if (enable) {
    const checked = await api<TUNStatus>("runtime/tun"); setStatus(checked);
    if (!checked.supported || !checked.ready) { setNotice(checked.message); return; }
   }
   await api("jobs", { method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({action: enable ? "tun-enable" : "tun-disable"}) });
   setPending(true); setNotice("TUN 任务已提交。请在后台任务页查看检查结果，关闭网页后任务仍会执行。");
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 return <section class="tun-control" aria-label="TUN 模式"><h3>TUN 模式</h3><p role="status">{status()?.enabled ? "已开启" : status() ? "已关闭" : "状态未知"} · {status()?.message || "正在检查内核状态…"}</p><p>开启后接管本机网络路由。初始配置仅直连，节点与规则沿用当前内核配置，不劫持 DNS；服务重启后默认关闭。</p><Show when={status()?.commands.length}><p>在后端所在主机的终端执行（需要管理员密码）：</p><For each={status()?.commands}>{command => <pre><code>{command}</code></pre>}</For><p>授权后停止并重新启动原服务，返回点击“检查并开启”。容器还需提供网络权限与 /dev/net/tun；内核升级后需重新授权。</p></Show><div class="actions"><button type="button" disabled={busy() || pending() || !status()?.supported || status()?.enabled} onClick={() => void toggle(true)}>检查并开启 TUN</button><button type="button" class="secondary" disabled={busy() || pending()} onClick={() => void toggle(false)}>关闭 TUN</button><a href="/tasks">查看任务结果 ↗</a></div><Show when={notice()}><p role="status">{notice()}</p></Show><Show when={error()}><p role="alert" class="error">{error()}</p></Show></section>;
}
