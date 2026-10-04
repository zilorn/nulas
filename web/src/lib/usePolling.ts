import { onCleanup, onMount } from "solid-js";
import { startVisiblePolling } from "./polling";

export function usePolling(refresh: () => Promise<void>, interval: number): void {
 onMount(() => onCleanup(startVisiblePolling(refresh, interval)));
}
