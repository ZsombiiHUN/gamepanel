import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // The panel blocks writes whose Origin doesn't match Host, so the browser's Host must reach it.
      // Vite 8's proxy sends the target's host even with changeOrigin off; put the original back.
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: false,
        ws: true,
        configure: (proxy) => {
          proxy.on("proxyReq", (proxyReq, req) => {
            if (req.headers.host) proxyReq.setHeader("host", req.headers.host);
          });
          proxy.on("proxyReqWs", (proxyReq, req) => {
            if (req.headers.host) proxyReq.setHeader("host", req.headers.host);
          });
        },
      },
    },
  },
  test: {
    environment: "node", // component tests opt in to jsdom with a `// @vitest-environment jsdom` comment
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["src/test/setup.ts"],
  },
});
