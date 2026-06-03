import tailwindcss from "@tailwindcss/vite";
import { devtools } from "@tanstack/devtools-vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const config = defineConfig({
	resolve: { tsconfigPaths: true },
	server: {
		// Fail loudly if the requested port is taken rather than drifting to
		// the next free one. A drifted port changes the page origin (e.g.
		// :3001), which the Go server's CSRF allowlist doesn't trust, so every
		// mutation 403s. Crashing on a busy port surfaces that immediately.
		strictPort: true,
		proxy: {
			"/api": {
				target: "http://localhost:8080",
				changeOrigin: true,
			},
			"/trpc": {
				target: "http://localhost:8080",
				changeOrigin: true,
			},
		},
	},
	plugins: [
		devtools(),
		tailwindcss(),
		tanstackStart({
			spa: {
				enabled: true,
				// Emit the prerendered shell as index.html so the Go server's SPA
				// fallback (and any static host) serves it directly, with no rename
				// step in the Docker build.
				prerender: {
					outputPath: "/index.html",
				},
			},
		}),
		viteReact(),
	],
});

export default config;
