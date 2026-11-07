import { defineConfig, mergeConfig } from "vitest/config";
import vite from "./vite.config";

// Ant Design's CSS-in-JS and portals need a bounded DOM-test budget in jsdom.
export default mergeConfig(
  vite,
  defineConfig({ test: { fileParallelism: false, testTimeout: 15000 } }),
);
