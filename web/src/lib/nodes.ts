export type Proxy = { name: string; type: string; now?: string; all?: string[] };

export function visibleGroups(nodes: Proxy[], mode: string): Proxy[] {
 if (mode !== "rule" && mode !== "global") return [];
 return nodes.filter(proxy => proxy.all?.length && (mode === "global" ? proxy.name === "GLOBAL" : proxy.name !== "GLOBAL"));
}

export function canChoose(group: Proxy, mode: string): boolean {
 return group.type === "Selector" && visibleGroups([group], mode).length > 0;
}
