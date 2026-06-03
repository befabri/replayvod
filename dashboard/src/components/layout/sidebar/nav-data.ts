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
import { authStore, hasRole } from "@/stores/auth";

// StaticRoute is the subset of router paths that take no params (no `$`
// segment). The sidebar only links to param-free destinations, so typing
// `to` against this lets `<Link to={...}>` type-check without params and
// without an `as any` cast — a renamed/removed route breaks the build here.
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
	ownerOnly?: boolean;
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
					{ to: "/dashboard/requests", label: t("nav.requests") },
					{ to: "/dashboard/activity/queue", label: t("nav.activity_queue") },
					{
						to: "/dashboard/activity/history",
						label: t("nav.activity_history"),
					},
				],
			},
			{
				icon: ShieldCheckIcon,
				label: t("nav.security"),
				ownerOnly: true,
				children: [
					{ to: "/dashboard/system/users", label: t("nav.users") },
					{ to: "/dashboard/system/whitelist", label: t("nav.whitelist") },
				],
			},
			{
				icon: DesktopIcon,
				label: t("nav.system"),
				ownerOnly: true,
				children: [
					{ to: "/dashboard/system/eventsub", label: t("nav.eventsub") },
					{ to: "/dashboard/system/webhook", label: t("nav.webhook") },
					{ to: "/dashboard/system/playback", label: t("nav.playback") },
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
		() => groups.filter((g) => !g.ownerOnly || hasRole(user, "owner")),
		[groups, user],
	);
}

/** True when the route is exactly `child.to` or nested below it. */
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
