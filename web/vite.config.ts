import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";
import { parse } from "yaml";

export default defineConfig(({ command }) => {
  // Production assets use the same origin; only the dev proxy needs this address.
  let target = "";
  let frontendPort = 5173;
  if (command === "serve") {
    const config = parse(
      readFileSync(new URL("../config.yaml", import.meta.url), "utf8"),
    );
    const { host, port } = config?.server ?? {};
    frontendPort = config?.frontend?.port ?? 5173;
    if (
      !Number.isInteger(frontendPort) ||
      frontendPort < 1 ||
      frontendPort > 65535
    ) {
      throw new Error("config.yaml 的 frontend.port 须为 1～65535 的整数");
    }
    if (
      typeof host !== "string" ||
      !host.trim() ||
      !Number.isInteger(port) ||
      port < 1 ||
      port > 65535
    ) {
      throw new Error("config.yaml 的 server.host 或 server.port 无效");
    }
    const proxyHost =
      host === "0.0.0.0" ? "127.0.0.1" : host === "::" ? "::1" : host.trim();
    target = `http://${proxyHost.includes(":") ? `[${proxyHost}]` : proxyHost}:${port}`;
  }
  return {
    plugins: [react()],
    server: {
      port: frontendPort,
      strictPort: true,
      proxy: {
        "/api": target,
        "/media": target,
      },
    },
  };
});
