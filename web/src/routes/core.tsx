import { useI18n } from "../lib/i18n";
import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import Workspace from "../components/Workspace";
import { api, type Job } from "../lib/api";
type Management = { version: string; core: { status: string; message: string }; managed: boolean; busy: boolean; platform: string };
type Release = { tag_name: string; published_at: string };
export default function Core() {
 const { t, locale } = useI18n();
 const [state, setState] = createSignal<Management>();
 const [releases, setReleases] = createSignal<Release[]>([]);
 const [page, setPage] = createSignal(1);
 const [hasMore, setHasMore] = createSignal(false);
 const [loading, setLoading] = createSignal(false);
 const [submitting, setSubmitting] = createSignal(false);
 const [error, setError] = createSignal("");
 const [releaseError, setReleaseError] = createSignal("");
 const [notice, setNotice] = createSignal("");
 const [job, setJob] = createSignal<Job>();
 let refreshing = false;
 async function refresh() {
  if (refreshing) return; refreshing = true;
  try {
   setState(await api<Management>("core/management")); setError("");
   if (job() && ["queued", "running"].includes(job()!.status)) {
    const jobs = await api<Job[]>("jobs"); const current = jobs.find(j => j.id === job()!.id); if (current) setJob(current);
   }
  } catch (e) { setError((e as Error).message); }
  finally { refreshing = false; }
 }
 async function loadReleases(next = 1) {
  if (loading()) return; setLoading(true); setReleaseError("");
  try { const data = await api<{ releases: Release[]; hasMore: boolean }>(`core/releases?page=${next}`); setReleases(next === 1 ? data.releases : [...releases(), ...data.releases]); setPage(next); setHasMore(data.hasMore); }
  catch (e) { setReleaseError((e as Error).message); }
  finally { setLoading(false); }
 }
 async function change(version: string) {
  if (submitting() || state()?.busy) return;
  setSubmitting(true); setError(""); setNotice("");
  try {
   const task = await api<Job>("core/switch", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ version }) });
   setJob(task); setNotice(t("已提交后台任务，关闭页面后仍会继续。切换期间连接会短暂中断。")); await refresh();
  } catch (e) { setError((e as Error).message); }
  finally { setSubmitting(false); }
 }
 const disabled = () => submitting() || !state()?.managed || state()?.busy || !!error();
 onMount(() => { void refresh(); void loadReleases(); const timer = setInterval(() => void refresh(), 2000); onCleanup(() => clearInterval(timer)); });
 return <Workspace page="core">
  <section class="intro"><div><span class="eyebrow">MIHOMO CORE</span><h1>{t("内核管理")}<span>{t("，随时更新。")}</span></h1><p>{t("查看官方稳定版本历史，选择版本切换，或一键更新到最新稳定版。")}</p></div><button disabled={disabled()} onClick={() => void change("latest")}>{state()?.busy || submitting() ? t("正在处理…") : t("一键更新")}</button></section>
  <Show when={error()}><p class="error" role="alert">{error()}</p></Show>
  <Show when={notice()}><p class="success" role="status">{notice()}</p></Show>
  <section class="panel core-overview"><div class="panel-heading"><div><h2>{t("托管内核 ·")}{state()?.version || t("版本未知 / 尚未安装")}</h2><p>{state()?.core.message || t("正在读取内核状态…")}</p></div><span class="badge">{state()?.platform || t("读取中")}</span></div>
   <p>{t("下载完成并校验后重启内核，保留已下载版本和已应用配置。启动检查失败会尝试恢复原版本。重启会中断现有连接，TUN 将关闭，需手动重新开启。")}</p>
   <Show when={state() && !state()?.managed}><p class="error">{t("当前为外部控制器模式；Nulas 无法切换或更新它的内核，请在外部自行管理。")}</p></Show>
   <Show when={state()?.busy}><p role="status">{t("内核任务正在后台执行，可在")}<a href="/tasks">{t("后台任务")}</a>{t("查看进度和失败原因。")}</p></Show>
   <Show when={job()}>{j => <p classList={{ error: j().status === "failed", success: j().status === "succeeded" }} role="status">{({ queued: t("等待执行"), running: t("执行中"), succeeded: t("已完成"), failed: t("失败") } as Record<string,string>)[j().status]} · {j().message}　<a href="/tasks">{t("查看任务 →")}</a></p>}</Show>
  </section>
  <section class="panel"><div class="panel-heading"><div><h2>{t("官方版本历史")}</h2><p>{t("稳定版本按官方发布时间排列。仅使用适配本机的官方预编译内核；缺少适配文件或校验信息时任务会失败。")}</p></div><button class="secondary" disabled={loading()} onClick={() => void loadReleases()}>{t("刷新版本")}</button></div>
   <Show when={releaseError()}><p class="error" role="alert">{releaseError()}</p></Show>
   <Show when={releases().length} fallback={<p role="status">{loading() ? t("正在读取官方版本…") : t("暂无版本，请刷新重试。")}</p>}>
    <div class="table-wrap"><table><thead><tr><th>{t("版本")}</th><th>{t("发布时间")}</th><th>{t("发布说明")}</th><th>{t("操作")}</th></tr></thead><tbody><For each={releases()}>{release => <tr><td><strong>{release.tag_name}</strong><Show when={state()?.version === release.tag_name}> <span class="badge">{t("当前版本")}</span></Show></td><td>{new Date(release.published_at).toLocaleDateString(locale())}</td><td><a href={`https://github.com/MetaCubeX/mihomo/releases/tag/${release.tag_name}`} target="_blank" rel="noreferrer">{t("查看说明 ↗")}</a></td><td><button class="secondary" disabled={disabled() || state()?.version === release.tag_name} onClick={() => void change(release.tag_name)}>{t("切换版本")}</button></td></tr>}</For></tbody></table></div>
   </Show>
   <Show when={hasMore()}><button class="secondary" disabled={loading()} onClick={() => void loadReleases(page() + 1)}>{loading() ? t("正在加载…") : t("加载更早版本")}</button></Show>
  </section>
 </Workspace>;
}
