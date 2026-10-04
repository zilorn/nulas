// Call only in the browser. The caller handles refresh failures and displays them.
export function startVisiblePolling(refresh: () => Promise<void>, interval: number, page: Pick<Document, "hidden" | "addEventListener" | "removeEventListener"> = document): () => void {
 let stopped = false;
 let running = false;
 const run = async () => {
  if (stopped || page.hidden || running) return;
  running = true;
  try { await refresh(); } finally { running = false; }
 };
 const visible = () => { if (!page.hidden) void run(); };
 page.addEventListener("visibilitychange", visible);
 const timer = setInterval(() => void run(), interval);
 void run();
 return () => {
  stopped = true;
  clearInterval(timer);
  page.removeEventListener("visibilitychange", visible);
 };
}
