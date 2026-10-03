import { createSignal, For, onMount, Show } from "solid-js";
import { api, defaultConfig, type Config } from "../lib/api";

type Profile = { id: string; name: string; config: Config; source: string; created: string };
const modes: Record<string, string> = { rule: "规则模式", global: "全局模式", direct: "直连模式" };
export default function ConfigurationManager(props: { onLoad: (config: Config) => void }) {
 const [profiles, setProfiles] = createSignal<Profile[]>([]);
 const [loading, setLoading] = createSignal(true);
 const [editor, setEditor] = createSignal<"create" | "import" | null>(null);
 const [name, setName] = createSignal("");
 const [config, setConfig] = createSignal(defaultConfig());
 const [content, setContent] = createSignal("");
 const [importKind, setImportKind] = createSignal<"local" | "network">("local");
 const [url, setURL] = createSignal("");
 const [busy, setBusy] = createSignal(false);
 const [error, setError] = createSignal("");
 const [notice, setNotice] = createSignal("");
 const [search, setSearch] = createSignal("");
 const refresh = async () => { setProfiles(await api<Profile[]>("profiles")); };
 onMount(() => { void refresh().catch(e => setError((e as Error).message)).finally(() => setLoading(false)); });
 const open = (kind: "create" | "import") => { setEditor(kind); setName(""); setContent(""); setURL(""); setImportKind("local"); setConfig(defaultConfig()); setError(""); setNotice(""); };
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
   const profile = await api<Profile>("profiles", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(editor() === "import" ? (importKind() === "network" ? { name: name(), url: url() } : { name: name(), content: content() }) : { name: name(), config: config() }) });
   setProfiles([...profiles(), profile]); setEditor(null); setNotice(`已保存「${profile.name}」。载入后可在快速配置页编辑或生成。`);
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
  <div class="library-summary"><span>已保存 <strong>{profiles().length}</strong> / 100 份配置</span><span>本地持久保存 · 载入后手动应用</span></div>
  <Show when={notice()}><p class="success" role="status">{notice()}</p></Show>
  <Show when={error()}><p class="error" role="alert">{error()}</p></Show>
  <Show when={editor()}><section class="panel profile-editor"><div class="panel-heading"><div><h2>{editor() === "import" ? "导入核心配置" : "创建核心配置"}</h2><p>保存到配置库，不会自动应用到内核。</p></div><button class="secondary" disabled={busy()} onClick={() => setEditor(null)}>取消</button></div>
   <form onSubmit={e => { e.preventDefault(); void save(); }}><fieldset disabled={busy()}>
    <label class="form-label">配置名称{networkImport() ? "（可选）" : ""}<input autofocus required={!networkImport()} maxlength={60} value={name()} onInput={e => setName(e.currentTarget.value)} placeholder={networkImport() ? "留空使用文件内的名称" : "例如：日常使用、局域网共享"} /></label>
    <Show when={editor() === "import"} fallback={<><div class="fields"><label>代理模式<select value={config().mode} onChange={e => update("mode", e.currentTarget.value)}><For each={Object.entries(modes)}>{([value, label]) => <option value={value}>{label}</option>}</For></select></label><label>混合端口<input type="number" required min="1" max="65535" value={config()["mixed-port"]} onInput={e => update("mixed-port", e.currentTarget.valueAsNumber)} /></label><label>日志级别<select value={config()["log-level"]} onChange={e => update("log-level", e.currentTarget.value)}><For each={["info", "warning", "error", "debug", "silent"]}>{level => <option>{level}</option>}</For></select></label></div><label class="toggle"><span>允许局域网连接</span><input type="checkbox" checked={config()["allow-lan"]} onChange={e => update("allow-lan", e.currentTarget.checked)} /></label><label class="toggle"><span>IPv6 支持</span><input type="checkbox" checked={config().ipv6} onChange={e => update("ipv6", e.currentTarget.checked)} /></label></>}>
     <div class="import-note">支持 JSON 对象或顶层标量 YAML（不限制文件大小），支持 mixed-port、mode、allow-lan、ipv6、log-level，以及可选的 name 名称字段。缺省值为 7890 / rule / false / false / info。包含节点、规则、订阅或密钥的完整配置会被拒绝。</div>
     <label class="form-label">导入方式<select value={importKind()} onChange={e => { setImportKind(e.currentTarget.value as "local" | "network"); setError(""); }}><option value="local">本地文件 / 粘贴内容</option><option value="network">网络地址</option></select></label>
     <Show when={importKind() === "local"} fallback={<><label class="form-label">配置地址<input type="url" required maxlength={4096} value={url()} onInput={e => setURL(e.currentTarget.value)} placeholder="https://example.com/config.yaml" /></label><p class="import-note">由后端一次性下载 HTTP/HTTPS 配置，15 秒超时，最多 5 次跳转。地址不会保存，不会定时更新订阅。名称留空时使用文件内的 name；文件未提供名称时使用“网络导入配置”。</p></>}>
     <label class="form-label">选择配置文件<input type="file" accept=".yaml,.yml,.json,application/json" onChange={e => void readFile(e.currentTarget.files?.[0])} /></label>
     <label class="form-label">配置内容<textarea required rows={8} value={content()} onInput={e => setContent(e.currentTarget.value)} placeholder={'mixed-port: 7890\nmode: rule\nallow-lan: false\nipv6: false\nlog-level: info'} spellcheck={false} /></label>
     </Show>
    </Show><div class="actions"><button type="submit">{busy() ? (editor() === "import" && importKind() === "network" ? "正在下载并保存…" : "正在保存…") : editor() === "import" ? "导入并保存" : "创建并保存"}</button></div>
   </fieldset></form>
  </section></Show>
  <section class="panel"><div class="panel-heading library-heading"><div><h2>我的配置</h2><p>载入会替换快速配置页的五项设置，已提交任务的快照保持独立。</p></div><label class="search-label">搜索配置<input type="search" placeholder="输入配置名称" value={search()} onInput={e => setSearch(e.currentTarget.value)} /></label></div>
   <Show when={!loading()} fallback={<p role="status">正在加载配置…</p>}><Show when={filtered().length > 0} fallback={<div class="empty">▤<p>{search() ? "没有匹配的配置" : "配置库还是空的"}</p><small>{search() ? "试试其他名称。" : "创建一份配置，或导入已有核心参数文件。"}</small></div>}>
    <div class="profile-grid"><For each={filtered()}>{profile => <article class="profile-card"><div class="profile-top"><span class="profile-icon">▤</span><span class="badge">{profile.source === "network" ? "网络导入" : profile.source === "imported" ? "导入" : "创建"}</span></div><h3>{profile.name}</h3><p>{modes[profile.config.mode]} · 端口 {profile.config["mixed-port"]}</p><div class="profile-tags"><span>{profile.config["allow-lan"] ? "局域网开放" : "仅本机"}</span><span>IPv6 {profile.config.ipv6 ? "开启" : "关闭"}</span><span>日志 {profile.config["log-level"]}</span></div><div class="profile-bottom"><small>{new Date(profile.created).toLocaleString("zh-CN")}</small><button class="secondary" disabled={busy()} onClick={() => void load(profile)}>载入配置 ↗</button></div></article>}</For></div>
   </Show></Show>
  </section>
 </>;
}
