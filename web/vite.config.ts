import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

const backend = "http://localhost:8700";

export default defineConfig({
  plugins: [solid()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: backend, changeOrigin: false },
      "/webhooks": { target: backend, changeOrigin: false },
    },
  },
  build: {
    outDir: "dist",
    target: "es2022",
    sourcemap: false,
  },
});
