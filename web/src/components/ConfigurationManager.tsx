import { useI18n } from "../lib/i18n";
import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { api, defaultConfig, type Config } from "../lib/api";

type Profile = { id: string; name: string; config: Config; source: string; created: string; full?: boolean; refreshable?: boolean; updateIntervalHours?: number; nextUpdate?: string; updatedAt?: string; refreshStatus?: string; refreshMessage?: string };
type AppliedConfig = Profile & { appliedAt: string; jobId: string };
const modes: Record<string, string> = { rule: "规则模式", global: "全局模式", direct: "直连模式" };
export default function ConfigurationManager(props: { onLoad: (config: Config) => void }) {
 const { t, locale } = useI18n();
 const [profiles, setProfiles] = createSignal<Profile[]>([]);
 const [applied, setApplied] = createSignal<AppliedConfig | null>(null);
 const [controller, setController] = createSignal(false);
 const [loading, setLoading] = createSignal(true);
 const [editor, setEditor] = createSignal<"create" | "import" | null>(null);
 const [name, setName] = createSignal("");
 const [config, setConfig] = createSignal(defaultConfig());
 const [content, setContent] = createSignal("");
 const [importKind, setImportKind] = createSignal<"local" | "network">("local");
 const [url, setURL] = createSignal("");
 const [intervalHours, setIntervalHours] = createSignal(0);
 const [updateEditor, setUpdateEditor] = createSignal<string | null>(null);
 const [updateURL, setUpdateURL] = createSignal("");
 const [updateHours, setUpdateHours] = createSignal(0);
 const [busy, setBusy] = createSignal(false);
 const [error, setError] = createSignal("");
 const [pollError, setPollError] = createSignal("");
 const [notice, setNotice] = createSignal("");
 const [search, setSearch] = createSignal("");
 const refresh = async () => { const [profiles, health, applied] = await Promise.all([api<Profile[]>("profiles"), api<{controllerConfigured: boolean}>("health"), api<AppliedConfig | null>("config/applied")]); setProfiles(current => JSON.stringify(current) === JSON.stringify(profiles) ? current : profiles); setController(health.controllerConfigured); setApplied(applied); };
 onMount(() => {
  void refresh().catch(e => setError((e as Error).message)).finally(() => setLoading(false));
  const timer = setInterval(() => { void refresh().then(() => setPollError("")).catch(e => { setController(false); setPollError(t("状态更新失败：{p0}", { p0: (e as Error).message })); }); }, 2000);
  onCleanup(() => clearInterval(timer));
 });
 const open = (kind: "create" | "import") => { setEditor(kind); setName(""); setContent(""); setURL(""); setImportKind("local"); setIntervalHours(0); setConfig(defaultConfig()); setError(""); setNotice(""); };
 const update = <K extends keyof Config>(key: K, value: Config[K]) => setConfig({ ...config(), [key]: value });
 async function readFile(file?: File) {
  if (!file) return;
  setError("");
  setBusy(true);
  try { setContent(await file.text()); if (!name()) setName(file.name.replace(/\.(ya?ml|json)$/i, "").slice(0, 60)); }
  catch { setError(t("文件读取失败，请重试或粘贴配置内容。")); }
  finally { setBusy(false); }
 }
 async function save() {
  setBusy(true); setError(""); setNotice("");
  try {
   const profile = await api<Profile>("profiles", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(editor() === "import" ? (importKind() === "network" ? { name: name(), url: url(), updateIntervalHours: intervalHours() } : { name: name(), content: content() }) : { name: name(), config: config() }) });
   setProfiles([...profiles(), profile]); setEditor(null); setNotice(t("已保存「{p0}」。{p1}", { p0: profile.name, p1: profile.full ? t("可在配置库生成或应用完整配置。") : t("载入后可在快速配置页编辑或生成。") }));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function submitProfile(profile: Profile, action: "generate" | "apply") {
  setBusy(true); setError(""); setNotice("");
  try {
   await api("jobs", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action, profileId: profile.id }) });
   setNotice(t("已提交「{p0}」的{p1}任务，请在后台任务页查看结果。", { p0: profile.name, p1: action === "apply" ? t("应用") : t("生成") }));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function refreshProfile(profile: Profile) {
  setBusy(true); setError(""); setNotice("");
  try {
   await api(`profiles/${profile.id}/refresh`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
   setNotice(t("已提交「{p0}」的更新任务。更新只保存到配置库，应用到内核需手动操作。", { p0: profile.name }));
   await refresh();
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function saveUpdates(profile: Profile) {
  setBusy(true); setError(""); setNotice("");
  try {
   await api(`profiles/${profile.id}/updates`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ updateIntervalHours: updateHours(), ...(updateURL().trim() ? { url: updateURL().trim() } : {}) }) });
   setUpdateEditor(null); setUpdateURL(""); await refresh(); setNotice(t("更新设置已保存，后端运行期间会按计划更新。"));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function load(profile: Profile) {
  setBusy(true); setError("");
  try { props.onLoad(await api<Config>(`profiles/${profile.id}/load`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" })); }
  catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 const networkImport = () => editor() === "import" && importKind() === "network";
 const filtered = () => profiles().filter(p => p.name.toLowerCase().includes(search().toLowerCase()));
 return <>
  <section class="intro config-intro"><div><h1>{t("配置管理")}</h1><p>{t("保存常用配置，按需载入或应用。")}</p></div><div class="manager-actions"><button class="secondary" disabled={loading() || busy()} onClick={() => open("import")}>{t("↓ 导入配置")}</button><button disabled={loading() || busy()} onClick={() => open("create")}>{t("＋ 创建配置")}</button></div></section>
  <div class="library-summary"><span>{t("已保存")}<strong>{profiles().length}</strong> {t("/ 100 份配置")}</span><a class="config-link" href="/tasks">{t("查看后台任务 ↗")}</a></div>
  <Show when={notice()}><p class="success" role="status">{notice()}</p></Show>
  <Show when={error() || pollError()}><p class="error" role="alert">{error() || pollError()}</p></Show>
  <Show when={applied()}>{current => <section class="panel applied-panel" aria-label={t("已应用配置")}><div class="panel-heading"><div><span class="applied-label">{t("上次成功应用")}</span><h2>{current().name}</h2><p>{t("最后成功应用于")}{new Date(current().appliedAt).toLocaleString(locale())}</p></div><span class="status succeeded">{t("已保存应用记录")}</span></div><p>{t(modes[current().config.mode] || current().config.mode)} {t("· 端口")}{current().config["mixed-port"]} · {current().full ? t("含完整配置") : t("核心参数配置")}</p><details class="config-details"><summary>{t("重启与记录说明")}</summary><p>{t("托管内核下次启动会加载这份配置，保留节点、代理组、规则与 DNS，随后按保存的开启偏好恢复 TUN。外部内核的重启配置由其自身管理；此处记录 Nulas 最后成功应用的结果，外部修改不会同步。")}</p></details></section>}</Show>
  <Show when={!loading() && !applied()}><p class="import-note">{t("尚无成功应用记录。创建、导入或保存编辑内容后，仍需点击应用。")}</p></Show>
  <Show when={editor()}><section class="panel profile-editor"><div class="panel-heading"><div><h2>{editor() === "import" ? t("导入配置") : t("创建核心配置")}</h2><p>{t("保存到配置库，不会自动应用到内核。")}</p></div><button class="secondary" disabled={busy()} onClick={() => setEditor(null)}>{t("取消")}</button></div>
   <form onSubmit={e => { e.preventDefault(); void save(); }}><fieldset disabled={busy()}>
    <label class="form-label">{t("配置名称")}{networkImport() ? t("（可选）") : ""}<input autofocus required={!networkImport()} maxlength={60} value={name()} onInput={e => setName(e.currentTarget.value)} placeholder={networkImport() ? t("留空使用文件内的名称") : t("例如：日常使用、局域网共享")} /></label>
    <Show when={editor() === "import"} fallback={<><div class="fields"><label>{t("代理模式")}<select value={config().mode} onChange={e => update("mode", e.currentTarget.value)}><For each={Object.entries(modes)}>{([value, label]) => <option value={value}>{t(label)}</option>}</For></select></label><label>{t("混合端口")}<input type="number" required min="1" max="65535" value={config()["mixed-port"]} onInput={e => update("mixed-port", e.currentTarget.valueAsNumber)} /></label><label>{t("日志级别")}<select value={config()["log-level"]} onChange={e => update("log-level", e.currentTarget.value)}><For each={["info", "warning", "error", "debug", "silent"]}>{level => <option>{level}</option>}</For></select></label></div><label class="toggle"><span>{t("允许局域网连接")}</span><input type="checkbox" checked={config()["allow-lan"]} onChange={e => update("allow-lan", e.currentTarget.checked)} /></label><label class="toggle"><span>{t("IPv6 支持")}</span><input type="checkbox" checked={config().ipv6} onChange={e => update("ipv6", e.currentTarget.checked)} /></label></>}>
     <details class="config-details import-help"><summary>{t("支持哪些配置格式？")}</summary><p>{t("支持 Mihomo YAML / JSON 配置，包括节点、代理组、规则、DNS 和 providers；支持嵌套、引号、注释及 YAML 锚点。完整配置保存在后端，导入不会自动应用。Base64 节点订阅、加密内容和多文档仍不支持。")}</p></details>
     <label class="form-label">{t("导入方式")}<select value={importKind()} onChange={e => { setImportKind(e.currentTarget.value as "local" | "network"); setError(""); }}><option value="local">{t("本地文件 / 粘贴内容")}</option><option value="network">{t("网络地址")}</option></select></label>
     <Show when={importKind() === "local"} fallback={<><label class="form-label">{t("配置地址")}<input type="url" required maxlength={4096} value={url()} onInput={e => setURL(e.currentTarget.value)} placeholder="https://example.com/config.yaml" /></label><p class="import-note">{t("由后端下载 HTTP/HTTPS 配置，15 秒超时，最多 5 次跳转。更新地址（可能含令牌）仅保存在后端，可立即更新或定时更新；关闭浏览器后仍可执行，后端须保持运行。更新不会自动应用到内核。名称留空时使用文件内的 name；文件未提供名称时使用“网络导入配置”。")}</p><label class="form-label">{t("定时更新间隔（小时，0 为关闭）")}<input type="number" required min="0" max="720" step="1" value={intervalHours()} onInput={e => setIntervalHours(e.currentTarget.valueAsNumber)} /></label></>}>
     <label class="form-label">{t("选择配置文件")}<input type="file" accept=".yaml,.yml,.json,application/json" onChange={e => void readFile(e.currentTarget.files?.[0])} /></label>
     <label class="form-label">{t("配置内容")}<textarea required rows={8} value={content()} onInput={e => setContent(e.currentTarget.value)} placeholder={'mixed-port: 7890\nmode: rule\nallow-lan: false\nipv6: false\nlog-level: info'} spellcheck={false} /></label>
     </Show>
    </Show><div class="actions"><button type="submit">{busy() ? (editor() === "import" && importKind() === "network" ? t("正在下载并保存…") : t("正在保存…")) : editor() === "import" ? t("导入并保存") : t("创建并保存")}</button></div>
   </fieldset></form>
  </section></Show>
  <section class="panel"><div class="panel-heading library-heading"><div><h2>{t("我的配置")}</h2><p>{t("核心参数可载入编辑，完整配置可直接应用。")}</p></div><label class="search-label">{t("搜索配置")}<input type="search" placeholder={t("输入配置名称")} value={search()} onInput={e => setSearch(e.currentTarget.value)} /></label></div>
   <div class="library-apply-note"><p>{t("应用完整配置会关闭 TUN、透明代理及自定义入站监听。")}</p><details class="config-details"><summary>{t("应用行为与生成说明")}</summary><p>{t("点击“应用配置”即可重载节点、代理组、规则和 DNS，无需先关闭 TUN 或透明代理。应用后代理监听跟随“允许局域网连接”设置，关闭 TUN、透明代理及自定义入站监听；系统代理设置、控制接口及其凭据保持不变。完整配置生成保留原文。")}</p></details></div>
   <Show when={!loading()} fallback={<p role="status">{t("正在加载配置…")}</p>}><Show when={filtered().length > 0} fallback={<div class="empty">▤<p>{search() ? t("没有匹配的配置") : t("配置库还是空的")}</p><small>{search() ? t("试试其他名称。") : t("创建一份配置，或导入已有核心参数文件。")}</small></div>}>
    <div class="profile-grid"><For each={filtered()}>{profile => <article class="profile-card" classList={{"was-applied":applied()?.id===profile.id}}><div class="profile-top"><span class="profile-icon">▤</span><div class="profile-badges"><span class="badge">{profile.source === "network" ? t("网络导入") : profile.source === "imported" ? t("导入") : t("创建")}</span><Show when={applied()?.id === profile.id}><span class="status succeeded">{t("上次已应用")}</span></Show></div></div><h3>{profile.name}</h3><span class="profile-kind">{profile.full ? t("完整配置") : t("核心参数")}</span><p>{t(modes[profile.config.mode] || profile.config.mode)} {t("· 端口")}{profile.config["mixed-port"]}</p><details class="config-details profile-parameters"><summary>{t("配置参数")}</summary><div class="profile-tags"><Show when={profile.full}><span>{t("完整配置")}</span></Show><span>{profile.config["allow-lan"] ? t("局域网开放") : t("仅本机")}</span><span>IPv6 {profile.config.ipv6 ? t("开启") : t("关闭")}</span><span>{t("日志")}{profile.config["log-level"]}</span></div></details>
     <Show when={profile.source === "network"}><div class="profile-updates">
      <p>{t("定时更新：")}{profile.updateIntervalHours ? t("每 {p0} 小时", { p0: profile.updateIntervalHours }) : t("已关闭")}</p>
      <Show when={profile.updatedAt}><small>{t("最近更新：")}{new Date(profile.updatedAt!).toLocaleString(locale())}</small></Show>
      <Show when={profile.nextUpdate}><small>{t("下次更新：")}{new Date(profile.nextUpdate!).toLocaleString(locale())}</small></Show>
      <Show when={profile.refreshMessage}><p class={profile.refreshStatus === "failed" ? "error" : "import-note"} role="status">{profile.refreshMessage}</p></Show>
      <Show when={!profile.refreshable}><p class="import-note">{t("此旧配置未保存地址，请在更新设置中补充。")}</p></Show>
      <div class="actions"><button class="secondary" disabled={busy() || !profile.refreshable || profile.refreshStatus === "queued"} onClick={() => void refreshProfile(profile)}>{profile.refreshStatus === "queued" ? t("正在更新…") : t("立即更新")}</button><button class="secondary" disabled={busy()} onClick={() => { setUpdateEditor(profile.id); setUpdateURL(""); setUpdateHours(profile.updateIntervalHours ?? 0); }}>{t("更新设置")}</button></div>
      <Show when={updateEditor() === profile.id}><form onSubmit={e => { e.preventDefault(); void saveUpdates(profile); }}><fieldset disabled={busy()}>
       <label class="form-label">{t("更新地址")}{profile.refreshable ? t("（留空保留原地址）") : ""}<input type="url" required={!profile.refreshable} maxlength={4096} value={updateURL()} onInput={e => setUpdateURL(e.currentTarget.value)} placeholder="https://example.com/config.yaml" /></label>
       <label class="form-label">{t("定时更新间隔（小时，0 为关闭）")}<input type="number" required min="0" max="720" step="1" value={updateHours()} onInput={e => setUpdateHours(e.currentTarget.valueAsNumber)} /></label>
       <p class="import-note">{t("更新仅刷新配置库，失败保留原配置；应用到内核需手动操作。")}</p><div class="actions"><button type="submit">{t("保存更新设置")}</button><button type="button" class="secondary" onClick={() => { setUpdateEditor(null); setUpdateURL(""); }}>{t("取消")}</button></div>
      </fieldset></form></Show>
     </div></Show><div class="profile-bottom"><small>{t("创建于")}{new Date(profile.created).toLocaleDateString(locale())}</small><div class="profile-actions"><Show when={profile.full} fallback={<button class="secondary" disabled={busy()} onClick={() => void load(profile)}>{t("载入编辑 ↗")}</button>}><button class="secondary" disabled={busy()} onClick={() => void submitProfile(profile, "generate")}>{t("生成")}</button><button disabled={busy() || !controller()} onClick={() => void submitProfile(profile, "apply")}>{t("应用配置")}</button></Show></div><Show when={profile.full && !controller()}><small class="profile-offline">{t("内核未连接，暂不可应用")}</small></Show></div></article>}</For></div>
   </Show></Show>
  </section>
 </>;
}
