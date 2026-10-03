import { createMiddleware } from "@solidjs/start/middleware";

// Go owns /api. Requests reaching the renderer must never receive an HTML page.
export default createMiddleware({
 onRequest(event) {
  const path = new URL(event.request.url).pathname;
  if (path === "/api" || path.startsWith("/api/")) {
   return Response.json({ error: "当前访问的是 SSR 内部端口，不提供 API。请使用生产 Web/API 入口（默认 http://127.0.0.1:4669，可通过 nulas config port 查询），并确认 Go 后端已启动。" }, { status: 503, headers: { "Cache-Control": "no-store" } });
  }
 },
});
