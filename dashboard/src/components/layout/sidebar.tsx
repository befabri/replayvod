import type { Icon } from "@phosphor-icons/react";
import {
	CaretDown,
	Clock,
	Desktop,
	Gear,
	House,
	ListChecks,
	Play,
	TrayArrowDown,
} from "@phosphor-icons/react";
import { Link, useRouterState } from "@tanstack/react-router";
import { useStore } from "@tanstack/react-store";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { authStore, hasRole } from "@/stores/auth";
import { closeSidebar, uiStore } from "@/stores/ui";

type NavChild = { to: string; label: string; exact?: boolean };
type NavGroup = {
	icon: Icon;
	label: string;
	ownerOnly?: boolean;
} & (
	| { to: string; children?: undefined }
	| { to?: undefined; children: NavChild[] }
);

function useNavGroups(): NavGroup[] {
	const { t } = useTranslation();
	return useMemo<NavGroup[]>(
		() => [
			{ icon: House, label: t("nav.dashboard"), to: "/dashboard" },
			{
				icon: Play,
				label: t("nav.videos"),
				children: [
					{ to: "/dashboard/videos", label: t("nav.videos"), exact: true },
					{ to: "/dashboard/categories", label: t("nav.categories") },
					{ to: "/dashboard/channels", label: t("nav.channels") },
				],
			},
			{
				icon: TrayArrowDown,
				label: t("nav.recording"),
				children: [
					{ to: "/dashboard/schedules", label: t("nav.schedules") },
					{ to: "/dashboard/requests", label: t("nav.requests") },
				],
			},
			{
				icon: ListChecks,
				label: t("nav.activity"),
				children: [
					{ to: "/dashboard/activity/queue", label: t("nav.activity_queue") },
					{
						to: "/dashboard/activity/history",
						label: t("nav.activity_history"),
					},
				],
			},
			{ icon: Gear, label: t("nav.settings"), to: "/dashboard/settings" },
			{ icon: Clock, label: t("nav.sessions"), to: "/dashboard/sessions" },
			{
				icon: Desktop,
				label: t("nav.system"),
				ownerOnly: true,
				children: [
					{ to: "/dashboard/system/users", label: t("nav.users") },
					{ to: "/dashboard/system/whitelist", label: t("nav.whitelist") },
					{ to: "/dashboard/system/eventsub", label: t("nav.eventsub") },
					{ to: "/dashboard/system/tasks", label: t("nav.tasks") },
					{ to: "/dashboard/system/events", label: t("nav.events") },
					{ to: "/dashboard/system/logs", label: t("nav.logs") },
				],
			},
		],
		[t],
	);
}

export function Sidebar() {
	const { t } = useTranslation();
	const user = useStore(authStore, (s) => s.user);
	const open = useStore(uiStore, (s) => s.sidebarOpen);
	const pathname = useRouterState({ select: (s) => s.location.pathname });
	const groups = useNavGroups();

	const visibleGroups = useMemo(
		() => groups.filter((g) => !g.ownerOnly || hasRole(user, "owner")),
		[groups, user],
	);

	const activeGroupIndex = useMemo(() => {
		return visibleGroups.findIndex((g) =>
			g.children?.some(
				(c) => pathname === c.to || pathname.startsWith(`${c.to}/`),
			),
		);
	}, [visibleGroups, pathname]);

	const [openIndex, setOpenIndex] = useState<number | null>(activeGroupIndex);

	useEffect(() => {
		if (activeGroupIndex >= 0) setOpenIndex(activeGroupIndex);
	}, [activeGroupIndex]);

	useEffect(() => {
		if (!open) return;
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") closeSidebar();
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, [open]);

	return (
		<>
			{open && (
				<button
					type="button"
					aria-label={t("nav.close_menu")}
					onClick={closeSidebar}
					className="md:hidden fixed inset-0 z-40 bg-overlay"
				/>
			)}

			<aside
				className={cn(
					"fixed left-0 top-0 bottom-0 w-56 pt-16 bg-sidebar text-sidebar-foreground border-r border-sidebar-border z-40 transition-transform duration-200 md:translate-x-0",
					open ? "translate-x-0" : "-translate-x-full",
				)}
			>
				<nav className="h-full overflow-y-auto px-3 pb-4 pt-3">
					<ul className="space-y-1">
						{visibleGroups.map((group, idx) => (
							<li key={group.label}>
								{group.children ? (
									<GroupItem
										group={group}
										isOpen={openIndex === idx}
										onToggle={() =>
											setOpenIndex((prev) => (prev === idx ? null : idx))
										}
									/>
								) : (
									<LeafLink
										to={group.to}
										icon={group.icon}
										label={group.label}
									/>
								)}
							</li>
						))}
					</ul>
				</nav>
			</aside>
		</>
	);
}

function GroupItem({
	group,
	isOpen,
	onToggle,
}: {
	group: NavGroup & { children: NavChild[] };
	isOpen: boolean;
	onToggle: () => void;
}) {
	const Icon = group.icon;
	return (
		<>
			<button
				type="button"
				onClick={onToggle}
				className="group flex w-full items-center gap-3 rounded-md p-2 text-sm font-medium transition-colors duration-75 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
				aria-expanded={isOpen}
			>
				<Icon size={20} weight="fill" />
				<span className="flex-1 text-left truncate">{group.label}</span>
				<CaretDown
					size={16}
					weight="bold"
					className={cn(
						"transition-transform duration-200",
						isOpen && "rotate-180",
					)}
				/>
			</button>
			{isOpen && (
				<ul className="mt-1 space-y-1">
					{group.children.map((child) => (
						<li key={child.to}>
							<Link
								// biome-ignore lint/suspicious/noExplicitAny: TanStack Router types don't narrow well on arbitrary string paths
								to={child.to as any}
								onClick={closeSidebar}
								activeOptions={child.exact ? { exact: true } : undefined}
								className="flex items-center rounded-md p-2 pl-11 text-sm transition-colors duration-75 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground truncate"
								activeProps={{
									className:
										"flex items-center rounded-md p-2 pl-11 text-sm font-medium bg-sidebar-accent text-sidebar-accent-foreground truncate",
								}}
							>
								{child.label}
							</Link>
						</li>
					))}
				</ul>
			)}
		</>
	);
}

function LeafLink({
	to,
	icon: IconCmp,
	label,
}: {
	to: string;
	icon: Icon;
	label: string;
}) {
	return (
		<Link
			// biome-ignore lint/suspicious/noExplicitAny: TanStack Router types don't narrow well on arbitrary string paths
			to={to as any}
			onClick={closeSidebar}
			className="flex items-center gap-3 rounded-md p-2 text-sm font-medium transition-colors duration-75 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
			activeProps={{
				className:
					"flex items-center gap-3 rounded-md p-2 text-sm font-medium bg-sidebar-accent text-sidebar-accent-foreground",
			}}
			activeOptions={{ exact: true }}
		>
			<IconCmp size={20} weight="fill" />
			<span className="flex-1 truncate">{label}</span>
		</Link>
	);
}
