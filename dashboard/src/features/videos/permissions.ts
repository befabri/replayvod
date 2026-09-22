import { useSelector } from "@tanstack/react-store";
import { authStore, hasRole } from "@/stores/auth";

export function useCanManageVideos(): boolean {
	return hasRole(
		useSelector(authStore, (s) => s.user),
		"admin",
	);
}
