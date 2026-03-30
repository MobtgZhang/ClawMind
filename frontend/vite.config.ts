import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath, URL } from "node:url";

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
        // 避免开发代理缓冲 SSE，导致思考/正文只在整段结束后才出现在页面上
        configure(proxy) {
          proxy.on("proxyRes", (proxyRes, _req, res) => {
            const ct = proxyRes.headers["content-type"];
            if (ct && String(ct).includes("text/event-stream")) {
              proxyRes.headers["cache-control"] = "no-cache, no-transform";
              proxyRes.headers["x-accel-buffering"] = "no";
              delete proxyRes.headers["content-length"];
              const out = res as { flushHeaders?: () => void };
              out.flushHeaders?.();
            }
          });
        },
      },
    },
  },
});
