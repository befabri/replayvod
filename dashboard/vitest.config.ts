import { fileURLToPath } from "node:url";
import { storybookTest } from "@storybook/addon-vitest/vitest-plugin";
import { playwright } from "@vitest/browser-playwright";
import {
	defineConfig,
	type TestProjectInlineConfiguration,
} from "vitest/config";

const storybookConfigDir = fileURLToPath(
	new URL("./.storybook", import.meta.url),
);

const storybookViteConfig = fileURLToPath(
	new URL("./.storybook/vite.config.ts", import.meta.url),
);

function storybookProject(
	name: string,
	initialGlobals: Record<string, string>,
): TestProjectInlineConfiguration {
	return {
		extends: storybookViteConfig,
		plugins: [storybookTest({ configDir: storybookConfigDir, initialGlobals })],
		test: {
			name,
			browser: {
				enabled: true,
				headless: true,
				provider: playwright({
					contextOptions: { locale: "en-US", timezoneId: "UTC" },
				}),
				instances: [{ browser: "chromium" }],
			},
		},
	};
}

export default defineConfig({
	resolve: { tsconfigPaths: true },
	test: {
		projects: [
			{
				extends: true,
				test: {
					name: "unit",
					include: ["**/*.{test,spec}.{ts,tsx}"],
					exclude: [
						"**/node_modules/**",
						"**/dist/**",
						"scripts/probes/**",
						"tests/**",
					],
				},
			},
			storybookProject("storybook", { theme: "dark", locale: "en" }),
			storybookProject("storybook-light-fr", { theme: "light", locale: "fr" }),
		],
	},
});
