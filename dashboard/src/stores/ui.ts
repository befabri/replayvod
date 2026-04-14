import { Store } from "@tanstack/store";

interface UIState {
	sidebarOpen: boolean;
}

export const uiStore = new Store<UIState>({ sidebarOpen: false });

export function openSidebar() {
	uiStore.setState((s) => ({ ...s, sidebarOpen: true }));
}

export function closeSidebar() {
	uiStore.setState((s) => ({ ...s, sidebarOpen: false }));
}

export function toggleSidebar() {
	uiStore.setState((s) => ({ ...s, sidebarOpen: !s.sidebarOpen }));
}
