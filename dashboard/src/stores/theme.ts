import { Store } from "@tanstack/store"

type Theme = "light" | "dark"

function getInitialTheme(): Theme {
  if (typeof window === "undefined") return "light"
  const stored = localStorage.getItem("theme")
  if (stored === "dark" || stored === "light") return stored
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light"
}

export const themeStore = new Store<{ theme: Theme }>({
  theme: getInitialTheme(),
})

export function toggleTheme() {
  themeStore.setState((s) => {
    const next = s.theme === "light" ? "dark" : "light"
    if (typeof window !== "undefined") {
      localStorage.setItem("theme", next)
      document.documentElement.classList.toggle("dark", next === "dark")
    }
    return { theme: next }
  })
}

export function initTheme() {
  const { theme } = themeStore.state
  if (typeof document !== "undefined") {
    document.documentElement.classList.toggle("dark", theme === "dark")
  }
}
