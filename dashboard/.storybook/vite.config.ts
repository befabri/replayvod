import tailwindcss from "@tailwindcss/vite";
import viteReact from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
	resolve: { tsconfigPaths: true },
	plugins: [tailwindcss(), viteReact()],
	optimizeDeps: { include: ["storybook/internal/core-events"] },
	define: { "import.meta.env.VITE_API_URL": JSON.stringify("") },
});
