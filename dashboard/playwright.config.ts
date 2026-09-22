import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "./tests",
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	workers: 1,
	reporter: "list",
	use: {
		baseURL: "http://127.0.0.1:39174",
		...devices["Desktop Chrome"],
		trace: "retain-on-failure",
	},
	webServer: {
		command: "npm run build && node tests/support/preview.mjs",
		url: "http://127.0.0.1:39174",
		reuseExistingServer: false,
		timeout: 180_000,
	},
});
