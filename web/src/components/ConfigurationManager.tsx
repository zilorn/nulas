import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { api, defaultConfig, type Config } from "../lib/api";

type Profile = { id: string; name: string; config: Config; source: string; created: string; full?: boolean; refreshable?: boolean; updateIntervalHours?: number; nextUpdate?: string; updatedAt?: string; refreshStatus?: string; refreshMessage?: string };
type AppliedConfig = Profile & { appliedAt: string; jobId: string };
const modes: Record<string, string> = { rule: "规则模式", global: "全局模式", direct: "直连模式" };
export default function ConfigurationManager(props: { onLoad: (config: Config) => void }) {
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
  const timer = setInterval(() => { void refresh().then(() => setPollError("")).catch(e => { setController(false); setPollError(`状态更新失败：${(e as Error).message}`); }); }, 2000);
  onCleanup(() => clearInterval(timer));
 });
 const open = (kind: "create" | "import") => { setEditor(kind); setName(""); setContent(""); setURL(""); setImportKind("local"); setIntervalHours(0); setConfig(defaultConfig()); setError(""); setNotice(""); };
 const update = <K extends keyof Config>(key: K, value: Config[K]) => setConfig({ ...config(), [key]: value });
 async function readFile(file?: File) {
  if (!file) return;
  setError("");
  setBusy(true);
  try { setContent(await file.text()); if (!name()) setName(file.name.replace(/\.(ya?ml|json)$/i, "").slice(0, 60)); }
  catch { setError("文件读取失败，请重试或粘贴配置内容。"); }
  finally { setBusy(false); }
 }
 async function save() {
  setBusy(true); setError(""); setNotice("");
  try {
   const profile = await api<Profile>("profiles", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(editor() === "import" ? (importKind() === "network" ? { name: name(), url: url(), updateIntervalHours: intervalHours() } : { name: name(), content: content() }) : { name: name(), config: config() }) });
   setProfiles([...profiles(), profile]); setEditor(null); setNotice(`已保存「${profile.name}」。${profile.full ? "可在配置库生成或应用完整配置。" : "载入后可在快速配置页编辑或生成。"}`);
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function submitProfile(profile: Profile, action: "generate" | "apply") {
  setBusy(true); setError(""); setNotice("");
  try {
   await api("jobs", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action, profileId: profile.id }) });
   setNotice(`已提交「${profile.name}」的${action === "apply" ? "应用" : "生成"}任务，请在后台任务页查看结果。`);
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function refreshProfile(profile: Profile) {
  setBusy(true); setError(""); setNotice("");
  try {
   await api(`profiles/${profile.id}/refresh`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
   setNotice(`已提交「${profile.name}」的更新任务。更新只保存到配置库，应用到内核需手动操作。`);
   await refresh();
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 async function saveUpdates(profile: Profile) {
  setBusy(true); setError(""); setNotice("");
  try {
   await api(`profiles/${profile.id}/updates`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ updateIntervalHours: updateHours(), ...(updateURL().trim() ? { url: updateURL().trim() } : {}) }) });
   setUpdateEditor(null); setUpdateURL(""); await refresh(); setNotice("更新设置已保存，后端运行期间会按计划更新。");
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
  <section class="intro"><div><span class="eyebrow">CONFIGURATION LIBRARY</span><h1>配置管理<span>，随时切换。</span></h1><p>保存不同场景的核心设置，让每次配置都有迹可循。</p></div><div class="manager-actions"><button class="secondary" disabled={loading() || busy()} onClick={() => open("import")}>↓ 导入配置</button><button disabled={loading() || busy()} onClick={() => open("create")}>＋ 创建配置</button></div></section>
  <div class="library-summary"><span>已保存 <strong>{profiles().length}</strong> / 100 份配置</span><span>应用成功后保存 · 托管内核重启自动加载</span></div>
  <Show when={notice()}><p class="success" role="status">{notice()}</p></Show>
  <Show when={error() || pollError()}><p class="error" role="alert">{error() || pollError()}</p></Show>
  <Show when={applied()}>{current => <section class="panel" aria-label="已应用配置"><div class="panel-heading"><div><h2>已应用配置 · {current().name}</h2><p>最后成功应用于 {new Date(current().appliedAt).toLocaleString("zh-CN")}</p></div><span class="status succeeded">已应用 · 已保存</span></div><p>{modes[current().config.mode]} · 端口 {current().config["mixed-port"]} · {current().full ? "含完整配置" : "核心参数配置"}</p><p class="import-note">托管内核下次启动会加载这份配置，保留节点、代理组、规则与 DNS，TUN 默认关闭。外部内核的重启配置由其自身管理；此处记录 Nulas 最后成功应用的结果，外部修改不会同步。</p></section>}</Show>
  <Show when={!loading() && !applied()}><p class="import-note">尚无成功应用记录。创建、导入或保存编辑内容后，仍需点击应用。</p></Show>
  <Show when={editor()}><section class="panel profile-editor"><div class="panel-heading"><div><h2>{editor() === "import" ? "导入配置" : "创建核心配置"}</h2><p>保存到配置库，不会自动应用到内核。</p></div><button class="secondary" disabled={busy()} onClick={() => setEditor(null)}>取消</button></div>
   <form onSubmit={e => { e.preventDefault(); void save(); }}><fieldset disabled={busy()}>
    <label class="form-label">配置名称{networkImport() ? "（可选）" : ""}<input autofocus required={!networkImport()} maxlength={60} value={name()} onInput={e => setName(e.currentTarget.value)} placeholder={networkImport() ? "留空使用文件内的名称" : "例如：日常使用、局域网共享"} /></label>
    <Show when={editor() === "import"} fallback={<><div class="fields"><label>代理模式<select value={config().mode} onChange={e => update("mode", e.currentTarget.value)}><For each={Object.entries(modes)}>{([value, label]) => <option value={value}>{label}</option>}</For></select></label><label>混合端口<input type="number" required min="1" max="65535" value={config()["mixed-port"]} onInput={e => update("mixed-port", e.currentTarget.valueAsNumber)} /></label><label>日志级别<select value={config()["log-level"]} onChange={e => update("log-level", e.currentTarget.value)}><For each={["info", "warning", "error", "debug", "silent"]}>{level => <option>{level}</option>}</For></select></label></div><label class="toggle"><span>允许局域网连接</span><input type="checkbox" checked={config()["allow-lan"]} onChange={e => update("allow-lan", e.currentTarget.checked)} /></label><label class="toggle"><span>IPv6 支持</span><input type="checkbox" checked={config().ipv6} onChange={e => update("ipv6", e.currentTarget.checked)} /></label></>}>
     <div class="import-note">支持 Mihomo YAML / JSON 配置，包括节点、代理组、规则、DNS 和 providers；支持嵌套、引号、注释及 YAML 锚点。完整配置保存在后端，导入不会自动应用。Base64 节点订阅、加密内容和多文档仍不支持。</div>
     <label class="form-label">导入方式<select value={importKind()} onChange={e => { setImportKind(e.currentTarget.value as "local" | "network"); setError(""); }}><option value="local">本地文件 / 粘贴内容</option><option value="network">网络地址</option></select></label>
     <Show when={importKind() === "local"} fallback={<><label class="form-label">配置地址<input type="url" required maxlength={4096} value={url()} onInput={e => setURL(e.currentTarget.value)} placeholder="https://example.com/config.yaml" /></label><p class="import-note">由后端下载 HTTP/HTTPS 配置，15 秒超时，最多 5 次跳转。更新地址（可能含令牌）仅保存在后端，可立即更新或定时更新；关闭浏览器后仍可执行，后端须保持运行。更新不会自动应用到内核。名称留空时使用文件内的 name；文件未提供名称时使用“网络导入配置”。</p><label class="form-label">定时更新间隔（小时，0 为关闭）<input type="number" required min="0" max="720" step="1" value={intervalHours()} onInput={e => setIntervalHours(e.currentTarget.valueAsNumber)} /></label></>}>
     <label class="form-label">选择配置文件<input type="file" accept=".yaml,.yml,.json,application/json" onChange={e => void readFile(e.currentTarget.files?.[0])} /></label>
     <label class="form-label">配置内容<textarea required rows={8} value={content()} onInput={e => setContent(e.currentTarget.value)} placeholder={'mixed-port: 7890\nmode: rule\nallow-lan: false\nipv6: false\nlog-level: info'} spellcheck={false} /></label>
     </Show>
    </Show><div class="actions"><button type="submit">{busy() ? (editor() === "import" && importKind() === "network" ? "正在下载并保存…" : "正在保存…") : editor() === "import" ? "导入并保存" : "创建并保存"}</button></div>
   </fieldset></form>
  </section></Show>
  <section class="panel"><div class="panel-heading library-heading"><div><h2>我的配置</h2><p>核心参数配置可载入快速配置页；完整配置直接生成或应用，已提交任务的快照保持独立。</p></div><label class="search-label">搜索配置<input type="search" placeholder="输入配置名称" value={search()} onInput={e => setSearch(e.currentTarget.value)} /></label></div>
   <p class="import-note">点击“直接应用”即可重载节点、代理组、规则和 DNS，无需先关闭 TUN 或透明代理。应用后使用本机监听，关闭 TUN、透明代理及自定义入站监听；系统代理设置、控制接口及其凭据保持不变。完整配置生成保留原文。<a href="/tasks">查看后台任务 ↗</a></p>
   <Show when={!loading()} fallback={<p role="status">正在加载配置…</p>}><Show when={filtered().length > 0} fallback={<div class="empty">▤<p>{search() ? "没有匹配的配置" : "配置库还是空的"}</p><small>{search() ? "试试其他名称。" : "创建一份配置，或导入已有核心参数文件。"}</small></div>}>
    <div class="profile-grid"><For each={filtered()}>{profile => <article class="profile-card"><div class="profile-top"><span class="profile-icon">▤</span><div class="profile-badges"><span class="badge">{profile.source === "network" ? "网络导入" : profile.source === "imported" ? "导入" : "创建"}</span><Show when={applied()?.id === profile.id}><span class="status succeeded">上次已应用</span></Show></div></div><h3>{profile.name}</h3><p>{modes[profile.config.mode]} · 端口 {profile.config["mixed-port"]}</p><div class="profile-tags"><Show when={profile.full}><span>完整配置</span></Show><span>{profile.config["allow-lan"] ? "局域网开放" : "仅本机"}</span><span>IPv6 {profile.config.ipv6 ? "开启" : "关闭"}</span><span>日志 {profile.config["log-level"]}</span></div>
     <Show when={profile.source === "network"}><div class="profile-updates">
      <p>定时更新：{profile.updateIntervalHours ? `每 ${profile.updateIntervalHours} 小时` : "已关闭"}</p>
      <Show when={profile.updatedAt}><small>最近更新：{new Date(profile.updatedAt!).toLocaleString("zh-CN")}</small></Show>
      <Show when={profile.nextUpdate}><small>下次更新：{new Date(profile.nextUpdate!).toLocaleString("zh-CN")}</small></Show>
      <Show when={profile.refreshMessage}><p class={profile.refreshStatus === "failed" ? "error" : "import-note"} role="status">{profile.refreshMessage}</p></Show>
      <Show when={!profile.refreshable}><p class="import-note">此旧配置未保存地址，请在更新设置中补充。</p></Show>
      <div class="actions"><button class="secondary" disabled={busy() || !profile.refreshable || profile.refreshStatus === "queued"} onClick={() => void refreshProfile(profile)}>{profile.refreshStatus === "queued" ? "正在更新…" : "立即更新"}</button><button class="secondary" disabled={busy()} onClick={() => { setUpdateEditor(profile.id); setUpdateURL(""); setUpdateHours(profile.updateIntervalHours ?? 0); }}>更新设置</button></div>
      <Show when={updateEditor() === profile.id}><form onSubmit={e => { e.preventDefault(); void saveUpdates(profile); }}><fieldset disabled={busy()}>
       <label class="form-label">更新地址{profile.refreshable ? "（留空保留原地址）" : ""}<input type="url" required={!profile.refreshable} maxlength={4096} value={updateURL()} onInput={e => setUpdateURL(e.currentTarget.value)} placeholder="https://example.com/config.yaml" /></label>
       <label class="form-label">定时更新间隔（小时，0 为关闭）<input type="number" required min="0" max="720" step="1" value={updateHours()} onInput={e => setUpdateHours(e.currentTarget.valueAsNumber)} /></label>
       <p class="import-note">更新仅刷新配置库，失败保留原配置；应用到内核需手动操作。</p><div class="actions"><button type="submit">保存更新设置</button><button type="button" class="secondary" onClick={() => { setUpdateEditor(null); setUpdateURL(""); }}>取消</button></div>
      </fieldset></form></Show>
     </div></Show><div class="profile-bottom"><small>{new Date(profile.created).toLocaleString("zh-CN")}</small><Show when={profile.full} fallback={<button class="secondary" disabled={busy()} onClick={() => void load(profile)}>载入配置 ↗</button>}><button class="secondary" disabled={busy()} onClick={() => void submitProfile(profile, "generate")}>生成</button><button disabled={busy() || !controller()} onClick={() => void submitProfile(profile, "apply")}>直接应用</button></Show></div></article>}</For></div>
   </Show></Show>
  </section>
 </>;
}
