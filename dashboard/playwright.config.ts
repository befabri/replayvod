import { defineConfig, devices } from "@playwright/test";

// E2E config for the dashboard SPA. The app talks to a same-origin `/trpc`
// endpoint (VITE_API_URL defaults to ""), so the auth specs mock that endpoint
// with page.route and never need the real Go backend. `npm run dev` serves the
// SPA on :3000.
export default defineConfig({
	testDir: "./tests",
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	workers: process.env.CI ? 1 : undefined,
	reporter: "html",
	use: {
		// Dedicated port: :3000 is often taken in dev (e.g. Grafana), so the e2e
		// server runs on its own strict port to avoid silently reusing it.
		baseURL: "http://localhost:39173",
		trace: "on-first-retry",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: {
		command: "npx vite dev --port 39173 --strictPort",
		url: "http://localhost:39173",
		reuseExistingServer: !process.env.CI,
		timeout: 120_000,
	},
});
