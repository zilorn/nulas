import { createSignal, onCleanup, onMount, Show } from "solid-js";
import { api, type Job } from "../lib/api";

type UpdateStatus = { supported: boolean; message: string; installed: string; latest: string; checked: number; available: boolean; restartRequired: boolean; busy: boolean; job: Job | null; error: string };

export default function AppUpdates() {
 const [status, setStatus] = createSignal<UpdateStatus>();
 const [error, setError] = createSignal("");
 const [submitting, setSubmitting] = createSignal(false);
 let disposed = false;
 let refreshing = false;
 const refresh = async () => {
  if (refreshing) return;
  refreshing = true;
  try { const result = await api<UpdateStatus>("updates"); if (!disposed) { setStatus(result); setError(""); } }
  catch (e) { if (!disposed) setError((e as Error).message); }
  finally { refreshing = false; }
 };
 const submit = async (action: "check" | "install", automatic = false) => {
  if (submitting()) return;
  setSubmitting(true);
  try {
   await api(`updates/${action}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ automatic }) });
   await refresh();
  } catch (e) { if (!disposed) setError((e as Error).message); }
  finally { if (!disposed) setSubmitting(false); }
 };
 onMount(() => {
  void (async () => { await refresh(); if (!disposed && status()?.supported) await submit("check", true); })();
  const timer = setInterval(() => { void refresh(); }, 5000);
  onCleanup(() => { disposed = true; clearInterval(timer); });
 });
 const busy = () => submitting() || status()?.busy;
 const failed = () => status()?.job?.status === "failed";
 const message = () => {
  const s = status();
  if (!s) return "正在读取更新状态…";
  if (s.busy) return s.job?.action === "update-app" ? "正在后台下载并构建更新…" : "正在后台检查更新…";
  if (failed()) return s.job?.message;
  if (s.restartRequired) return "更新已安装，重启 Nulas 后生效";
  return s.message;
 };
 return <section class="sidebar-updates" aria-label="Nulas 软件更新">
  <div class="sidebar-update-heading"><strong>软件更新</strong><Show when={status()?.available && !status()?.restartRequired}><span class="update-dot" title="发现更新" /></Show></div>
  <p role="status" aria-live="polite" classList={{ error: failed() }}>{message()}</p>
  <Show when={failed() && status()?.restartRequired}><p>安装版本已切换，重启 Nulas 后生效。</p></Show>
  <Show when={error()}><p class="error">{error()}</p></Show>
  <Show when={status()?.supported}>
   <small>已安装 {status()?.installed.slice(0, 7)}<Show when={status()?.available}> → {status()?.latest.slice(0, 7)}</Show></small>
   <Show when={status()?.checked}><small>上次检查 {new Date((status()?.checked ?? 0) * 1000).toLocaleString("zh-CN")}</small></Show>
   <Show when={status()?.error && !failed()}><p class="error">{status()?.error}</p></Show>
   <div class="sidebar-update-actions"><button class="secondary" disabled={busy()} onClick={() => void submit("check")}>检查更新</button><Show when={status()?.available && !status()?.restartRequired}><button disabled={busy()} onClick={() => void submit("install")}>{status()?.job?.action === "update-app" && failed() ? "重试更新" : "执行更新"}</button></Show></div>
   <Show when={status()?.available || status()?.restartRequired}><small>更新后请手动重启服务；不会自动重启。</small></Show>
  </Show>
 </section>;
}
