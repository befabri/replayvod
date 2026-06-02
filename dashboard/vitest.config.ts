import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

export default defineConfig({
	resolve: {
		alias: {
			"@": fileURLToPath(new URL("./src", import.meta.url)),
		},
	},
	test: {
		include: ["**/*.{test,spec}.{ts,tsx}"],
		// `tests/` holds Playwright specs (run via `playwright test`), not vitest.
		exclude: [
			"**/node_modules/**",
			"**/dist/**",
			"scripts/probes/**",
			"tests/**",
		],
	},
});
