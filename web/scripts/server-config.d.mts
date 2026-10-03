export type Ports = { port: number; "ssr-port": number; "dev-port": number };
export function configPath(environment?: NodeJS.ProcessEnv, platform?: string, home?: string): string;
export function readPorts(path?: string): Ports;
export function ssrSettings(ports?: Ports, environment?: NodeJS.ProcessEnv): { host: string; port: number };
