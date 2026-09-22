import tailwindcss from "@tailwindcss/vite";
import { devtools } from "@tanstack/devtools-vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const config = defineConfig({
	resolve: { tsconfigPaths: true },
	server: {
		strictPort: true,
		proxy: {
			"/api": {
				target: "http://localhost:8080",
				changeOrigin: true,
			},
			"/trpc": {
				ws: true,
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
				prerender: {
					outputPath: "/index.html",
				},
			},
		}),
		viteReact(),
	],
});

export default config;
