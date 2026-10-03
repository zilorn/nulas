export type Proxy = { name: string; type: string; now?: string; all?: string[] };

export function visibleGroups(nodes: Proxy[], mode: string, fallbackTarget?: string): Proxy[] {
 if (mode !== "rule" && mode !== "global") return [];
 const groups = nodes.filter(proxy => proxy.all?.length && (mode === "global" ? proxy.name === "GLOBAL" : proxy.name !== "GLOBAL"));
 if (mode !== "rule" || !fallbackTarget) return groups;
 const fallbackIndex = groups.findIndex(proxy => proxy.name === fallbackTarget);
 if (fallbackIndex <= 0) return groups;
 return [groups[fallbackIndex], ...groups.slice(0, fallbackIndex), ...groups.slice(fallbackIndex + 1)];
}

export function canChoose(group: Proxy, mode: string): boolean {
 return group.type === "Selector" && visibleGroups([group], mode).length > 0;
}

export function canTest(proxy: Proxy | undefined): boolean {
 return !!proxy && !["Reject", "RejectDrop", "Pass"].includes(proxy.type);
}
