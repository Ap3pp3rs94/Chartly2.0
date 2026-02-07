import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api/gateway": {
        target: "http://localhost:8080",
        changeOrigin: true,
        secure: false,
      },
      "/api/storage": {
        target: "http://localhost:8083",
        changeOrigin: true,
        secure: false,
      },
      "/api/analytics": {
        target: "http://localhost:8081",
        changeOrigin: true,
        secure: false,
      },
      "/api/audit": {
        target: "http://localhost:8084",
        changeOrigin: true,
        secure: false,
      },
      "/api/auth": {
        target: "http://localhost:8085",
        changeOrigin: true,
        secure: false,
      },
      "/api/observer": {
        target: "http://localhost:8086",
        changeOrigin: true,
        secure: false,
      },
    },
  },
});
