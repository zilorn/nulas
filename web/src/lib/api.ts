export type Config = { "mixed-port": number; mode: string; "allow-lan": boolean; ipv6: boolean; "log-level": string };
export type Job = { id: string; action: string; status: string; message: string; created: string };
export async function api<T>(path: string, options?: RequestInit): Promise<T> {
 const response = await fetch(`/api/${path}`, options);
 const data = await response.json();
 if (!response.ok) throw new Error(data.error || "请求失败");
 return data;
}
export const defaultConfig = (): Config => ({"mixed-port":7890,mode:"rule","allow-lan":false,ipv6:false,"log-level":"info"});
