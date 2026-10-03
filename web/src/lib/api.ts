export type Config = { "mixed-port": number; mode: string; "allow-lan": boolean; ipv6: boolean; "log-level": string };
export type Job = { id: string; action: string; status: string; message: string; created: string };
export async function api<T>(path: string, options?: RequestInit): Promise<T> {
 const response = await fetch(`/api/${path}`, options);
 const contentType = response.headers.get("Content-Type")?.split(";", 1)[0].trim().toLowerCase();
 if (contentType !== "application/json") {
  throw new Error("API 返回了非 JSON 响应，请使用生产 Web/API 入口（通过 nulas config port 查询），并确认 Go 后端已启动；SSR 端口不提供 API。");
 }
 let data;
 try { data = await response.json(); }
 catch { throw new Error("API 返回了无效的 JSON 响应，请检查后端服务。"); }
 if (!response.ok) throw new Error(typeof data?.error === "string" ? data.error : `请求失败（HTTP ${response.status}）`);
 return data;
}
export const defaultConfig = (): Config => ({"mixed-port":7890,mode:"rule","allow-lan":false,ipv6:false,"log-level":"info"});
