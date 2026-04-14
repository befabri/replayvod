import { defineConfig } from "vite"
import { devtools } from "@tanstack/devtools-vite"
import { tanstackStart } from "@tanstack/react-start/plugin/vite"
import viteReact from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

const config = defineConfig({
	resolve: { tsconfigPaths: true },
	server: {
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
			},
		}),
		viteReact(),
	],
})

export default config
