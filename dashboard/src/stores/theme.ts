import { Store } from "@tanstack/store";

type Theme = "light" | "dark";

function getInitialTheme(): Theme {
	if (typeof window === "undefined") return "dark";
	const stored = window.localStorage.getItem("theme");
	if (stored === "dark" || stored === "light") return stored;
	return "dark";
}

export const themeStore = new Store<{ theme: Theme }>({
	theme: getInitialTheme(),
});

function applyTheme(theme: Theme) {
	if (typeof document === "undefined") return;
	const root = document.documentElement;
	root.classList.toggle("light", theme === "light");
	root.setAttribute("data-theme", theme);
}

export function setTheme(theme: Theme) {
	themeStore.setState(() => {
		if (typeof window !== "undefined") {
			window.localStorage.setItem("theme", theme);
		}
		applyTheme(theme);
		return { theme };
	});
}

export function toggleTheme() {
	setTheme(themeStore.state.theme === "dark" ? "light" : "dark");
}

export function initTheme() {
	applyTheme(themeStore.state.theme);
}
