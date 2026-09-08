import { defineConfig, devices } from "@playwright/test";

// E2E config for the dashboard SPA, run against the shipped bundle: the web
// server builds the app and serves dist/client the way the Go server does
// (static files, history fallback), so the browser suite exercises what users
// run, with no dev-server compilation or devtools in the way. The app talks
// to a same-origin `/trpc` endpoint (VITE_API_URL defaults to ""), which the
// specs mock with page.route; no Go backend is needed.
export default defineConfig({
	testDir: "./tests",
	// One shared server keeps parallel workers from contending; serial keeps
	// this small suite deterministic locally and in CI.
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	workers: 1,
	reporter: "list",
	use: {
		// Dedicated port: :3000 is often taken in dev (e.g. Grafana), so the e2e
		// server runs on its own strict port to avoid silently reusing it.
		baseURL: "http://127.0.0.1:39174",
		...devices["Desktop Chrome"],
		trace: "retain-on-failure",
	},
	webServer: {
		// Always rebuild so the suite never runs against a stale bundle.
		command: "npm run build && node tests/support/preview.mjs",
		url: "http://127.0.0.1:39174",
		reuseExistingServer: false,
		timeout: 180_000,
	},
});
