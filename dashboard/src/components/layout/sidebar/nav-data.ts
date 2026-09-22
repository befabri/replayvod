import type { Icon } from "@phosphor-icons/react";
import {
	DesktopIcon,
	DownloadSimpleIcon,
	HouseIcon,
	PlayIcon,
	ShieldCheckIcon,
} from "@phosphor-icons/react";
import { useSelector } from "@tanstack/react-store";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { FileRouteTypes } from "@/routeTree.gen";
import { authStore, hasRole, type Role } from "@/stores/auth";

export type StaticRoute = Exclude<FileRouteTypes["to"], `${string}$${string}`>;

export type NavChild = {
	to: StaticRoute;
	label: string;
	exact?: boolean;
	activePrefixes?: string[];
};

export type NavGroup = {
	icon: Icon;
	label: string;
	roleMin?: Role;
} & (
	| { to: StaticRoute; children?: undefined }
	| { to?: undefined; children: NavChild[] }
);

export function useNavGroups(): NavGroup[] {
	const { t } = useTranslation();
	return useMemo<NavGroup[]>(
		() => [
			{ icon: HouseIcon, label: t("nav.dashboard"), to: "/dashboard" },
			{
				icon: PlayIcon,
				label: t("nav.library"),
				children: [
					{
						to: "/dashboard/videos",
						label: t("nav.videos"),
						exact: true,
						activePrefixes: ["/dashboard/watch"],
					},
					{ to: "/dashboard/channels", label: t("nav.channels") },
					{ to: "/dashboard/categories", label: t("nav.categories") },
				],
			},
			{
				icon: DownloadSimpleIcon,
				label: t("nav.recordings"),
				children: [
					{ to: "/dashboard/schedules", label: t("nav.schedules") },
					{ to: "/dashboard/downloads", label: t("nav.downloads") },
					{ to: "/dashboard/archive", label: t("nav.archive") },
					{
						to: "/dashboard/activity/history",
						label: t("nav.activity_history"),
					},
				],
			},
			{
				icon: ShieldCheckIcon,
				label: t("nav.security"),
				roleMin: "admin",
				children: [
					{ to: "/dashboard/system/users", label: t("nav.users") },
					{ to: "/dashboard/system/whitelist", label: t("nav.whitelist") },
				],
			},
			{
				icon: DesktopIcon,
				label: t("nav.system"),
				roleMin: "owner",
				children: [
					{ to: "/dashboard/system/eventsub", label: t("nav.eventsub") },
					{ to: "/dashboard/system/webhook", label: t("nav.webhook") },
					{ to: "/dashboard/system/playback", label: t("nav.playback") },
					{ to: "/dashboard/system/storage", label: t("nav.storage") },
					{ to: "/dashboard/system/twitch", label: t("nav.twitch_playback") },
					{ to: "/dashboard/system/tasks", label: t("nav.tasks") },
					{ to: "/dashboard/system/logs", label: t("nav.logs") },
				],
			},
		],
		[t],
	);
}

export function useVisibleNavGroups(): NavGroup[] {
	const user = useSelector(authStore, (s) => s.user);
	const groups = useNavGroups();
	return useMemo(
		() => groups.filter((g) => !g.roleMin || hasRole(user, g.roleMin)),
		[groups, user],
	);
}

export function isChildActive(pathname: string, child: NavChild): boolean {
	if (
		child.activePrefixes?.some(
			(prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`),
		)
	) {
		return true;
	}
	if (child.exact) return pathname === child.to;
	return pathname === child.to || pathname.startsWith(`${child.to}/`);
}

export function isGroupActive(pathname: string, group: NavGroup): boolean {
	if (group.children)
		return group.children.some((c) => isChildActive(pathname, c));
	return pathname === group.to || pathname.startsWith(`${group.to}/`);
}

export function activeGroupIndex(pathname: string, groups: NavGroup[]): number {
	return groups.findIndex((g) => isGroupActive(pathname, g));
}
