import { defineMain } from "@storybook/react-vite/node";

export default defineMain({
	stories: ["../src/**/*.stories.@(ts|tsx)"],
	addons: [
		"@storybook/addon-docs",
		"@storybook/addon-a11y",
		"@storybook/addon-vitest",
	],
	framework: {
		name: "@storybook/react-vite",
		options: {
			builder: { viteConfigPath: ".storybook/vite.config.ts" },
		},
	},
	staticDirs: [{ from: "./media", to: "/api/v1/thumbnails" }],
	core: { disableTelemetry: true },
});
