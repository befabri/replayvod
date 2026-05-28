import { Store } from "@tanstack/store";

const COLLAPSED_KEY = "rv:sidebar-collapsed";

function loadCollapsed(): boolean {
	if (typeof window === "undefined") return false;
	return window.localStorage.getItem(COLLAPSED_KEY) === "1";
}

interface UIState {
	/** Mobile drawer visibility. */
	sidebarOpen: boolean;
	/** Desktop rail mode (icons only). Persisted across sessions. */
	sidebarCollapsed: boolean;
}

export const uiStore = new Store<UIState>({
	sidebarOpen: false,
	sidebarCollapsed: loadCollapsed(),
});

export function openSidebar() {
	uiStore.setState((s) => ({ ...s, sidebarOpen: true }));
}

export function closeSidebar() {
	uiStore.setState((s) => ({ ...s, sidebarOpen: false }));
}

export function toggleSidebar() {
	uiStore.setState((s) => ({ ...s, sidebarOpen: !s.sidebarOpen }));
}

export function setSidebarCollapsed(collapsed: boolean) {
	if (typeof window !== "undefined")
		window.localStorage.setItem(COLLAPSED_KEY, collapsed ? "1" : "0");
	uiStore.setState((s) => ({ ...s, sidebarCollapsed: collapsed }));
}

export function toggleSidebarCollapsed() {
	setSidebarCollapsed(!uiStore.state.sidebarCollapsed);
}
