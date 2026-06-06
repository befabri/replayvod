import { useSelector } from "@tanstack/react-store";
import { authStore, hasRole } from "@/stores/auth";

// Video management mutations (trigger, cancel, delete) are admin-only on the
// server. Use one hook for every matching UI control so viewers get a read-only
// surface instead of controls that fail after click/confirm.
export function useCanManageVideos(): boolean {
	return hasRole(
		useSelector(authStore, (s) => s.user),
		"admin",
	);
}
