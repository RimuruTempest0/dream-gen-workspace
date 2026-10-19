import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";
export default defineConfig({
  testDir: "tests",
  workers: 1,
  maxFailures: 1,
  fullyParallel: false,
  timeout: 45000,
  use: { trace: "retain-on-failure" },
  reporter: [
    ["list"],
    [
      "json",
      {
        outputFile: fileURLToPath(
          new URL("../docs/ui-test-results.json", import.meta.url),
        ),
      },
    ],
  ],
});
