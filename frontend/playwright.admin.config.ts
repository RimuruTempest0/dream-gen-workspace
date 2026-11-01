import { defineConfig } from "@playwright/test";

// Desktop QA for the admin tool (scripts/admin-qa.ps1 starts it on port 9223).
export default defineConfig({
  testDir: "tests-admin",
  workers: 1,
  maxFailures: 1,
  fullyParallel: false,
  timeout: 90000,
  use: { trace: "retain-on-failure" },
  reporter: [["list"]],
});
