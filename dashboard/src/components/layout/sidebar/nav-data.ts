import type { Icon } from "@phosphor-icons/react";
import {
	Desktop,
	DownloadSimple,
	FilmReel,
	House,
	ShieldCheck,
} from "@phosphor-icons/react";
import { useStore } from "@tanstack/react-store";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { authStore, hasRole } from "@/stores/auth";

export type NavChild = { to: string; label: string; exact?: boolean };

export type NavGroup = {
	icon: Icon;
	label: string;
	ownerOnly?: boolean;
} & (
	| { to: string; children?: undefined }
	| { to?: undefined; children: NavChild[] }
);

export function useNavGroups(): NavGroup[] {
	const { t } = useTranslation();
	return useMemo<NavGroup[]>(
		() => [
			{ icon: House, label: t("nav.dashboard"), to: "/dashboard" },
			{
				icon: FilmReel,
				label: t("nav.library"),
				children: [
					{ to: "/dashboard/videos", label: t("nav.videos"), exact: true },
					{ to: "/dashboard/channels", label: t("nav.channels") },
					{ to: "/dashboard/categories", label: t("nav.categories") },
				],
			},
			{
				icon: DownloadSimple,
				label: t("nav.recordings"),
				children: [
					{ to: "/dashboard/schedules", label: t("nav.schedules") },
					{ to: "/dashboard/requests", label: t("nav.requests") },
					{ to: "/dashboard/activity/queue", label: t("nav.activity_queue") },
					{
						to: "/dashboard/activity/history",
						label: t("nav.activity_history"),
					},
				],
			},
			{
				icon: ShieldCheck,
				label: t("nav.security"),
				ownerOnly: true,
				children: [
					{ to: "/dashboard/system/users", label: t("nav.users") },
					{ to: "/dashboard/system/whitelist", label: t("nav.whitelist") },
				],
			},
			{
				icon: Desktop,
				label: t("nav.system"),
				ownerOnly: true,
				children: [
					{ to: "/dashboard/system/eventsub", label: t("nav.eventsub") },
					{ to: "/dashboard/system/tasks", label: t("nav.tasks") },
					{ to: "/dashboard/system/logs", label: t("nav.logs") },
				],
			},
		],
		[t],
	);
}

export function useVisibleNavGroups(): NavGroup[] {
	const user = useStore(authStore, (s) => s.user);
	const groups = useNavGroups();
	return useMemo(
		() => groups.filter((g) => !g.ownerOnly || hasRole(user, "owner")),
		[groups, user],
	);
}

/** True when the route is exactly `child.to` or nested below it. */
export function isChildActive(pathname: string, child: NavChild): boolean {
	if (child.exact) return pathname === child.to;
	return pathname === child.to || pathname.startsWith(`${child.to}/`);
}

/** True when any child of the group matches the current route. */
export function isGroupActive(pathname: string, group: NavGroup): boolean {
	if (group.children)
		return group.children.some((c) => isChildActive(pathname, c));
	return pathname === group.to || pathname.startsWith(`${group.to}/`);
}

/** Index of the group owning the current route, or -1. */
export function activeGroupIndex(pathname: string, groups: NavGroup[]): number {
	return groups.findIndex((g) => isGroupActive(pathname, g));
}
