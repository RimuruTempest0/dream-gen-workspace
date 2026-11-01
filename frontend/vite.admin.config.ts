import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The admin tool (cmd/admin) shares this toolchain; it builds into cmd/admin/dist.
export default defineConfig({
  plugins: [react()],
  root: "admin",
  base: "./",
  build: { outDir: "../../cmd/admin/dist", emptyOutDir: true },
});
