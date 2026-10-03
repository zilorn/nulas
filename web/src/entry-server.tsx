import { createHandler, StartServer } from "@solidjs/start/server";
export default createHandler(() => <StartServer document={({ assets, children, scripts }) => <html lang="zh-CN"><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/><title>Nulas · 快速配置</title>{assets}</head><body><div id="app">{children}</div>{scripts}</body></html>} />);
