import { CaretDoubleLeft, CaretDown } from "@phosphor-icons/react";
import { Link, useRouterState } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
	closeSidebar,
	setSidebarCollapsed,
	toggleSidebarCollapsed,
	uiStore,
} from "@/stores/ui";
import {
	activeGroupIndex,
	isChildActive,
	isGroupActive,
	type NavChild,
	type NavGroup,
	type StaticRoute,
	useVisibleNavGroups,
} from "./nav-data";

// Single source of truth for the sidebar footprint. The aside width and the
// layout's <main> margin must move together, so both live here and are imported
// by the layout rather than re-typed there.
export const SIDEBAR_EXPANDED = "w-64";
export const SIDEBAR_COLLAPSED = "w-[4.5rem]";
export const SIDEBAR_MARGIN_EXPANDED = "md:ml-64";
export const SIDEBAR_MARGIN_COLLAPSED = "md:ml-[4.5rem]";
export const SIDEBAR_EASE = "ease-[cubic-bezier(0.32,0.72,0,1)]";

function useIsDesktop() {
	const [desktop, setDesktop] = useState(true);
	useEffect(() => {
		const mq = window.matchMedia("(min-width: 768px)");
		const update = () => setDesktop(mq.matches);
		update();
		mq.addEventListener("change", update);
		return () => mq.removeEventListener("change", update);
	}, []);
	return desktop;
}

export function Sidebar() {
	const { t } = useTranslation();
	const open = useSelector(uiStore, (s) => s.sidebarOpen);
	const collapsed = useSelector(uiStore, (s) => s.sidebarCollapsed);
	const groups = useVisibleNavGroups();
	const pathname = useRouterState({ select: (s) => s.location.pathname });
	const isDesktop = useIsDesktop();

	// Rail mode only applies on desktop; the mobile drawer is always full width.
	const compact = collapsed && isDesktop;

	const activeIndex = useMemo(
		() => activeGroupIndex(pathname, groups),
		[pathname, groups],
	);
	const [openIndex, setOpenIndex] = useState<number | null>(activeIndex);

	useEffect(() => {
		if (activeIndex >= 0) setOpenIndex(activeIndex);
	}, [activeIndex]);

	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") closeSidebar();
			if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "b") {
				e.preventDefault();
				toggleSidebarCollapsed();
			}
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, []);

	// Expand the rail and reveal a group's children in one click.
	const expandInto = (idx: number) => {
		setSidebarCollapsed(false);
		setOpenIndex(idx);
	};

	return (
		<>
			{open && (
				<button
					type="button"
					aria-label={t("nav.close_menu")}
					onClick={closeSidebar}
					className="md:hidden fixed inset-0 z-40 bg-overlay backdrop-blur-[2px] animate-in fade-in duration-200"
				/>
			)}

			<aside
				className={cn(
					"fixed left-0 top-16 bottom-0 z-40 flex flex-col bg-sidebar text-sidebar-foreground border-r border-sidebar-border",
					"transition-[width,transform] duration-300 md:translate-x-0",
					SIDEBAR_EASE,
					compact ? SIDEBAR_COLLAPSED : SIDEBAR_EXPANDED,
					open ? "translate-x-0" : "-translate-x-full",
				)}
			>
				<TooltipProvider>
					<nav className="flex-1 overflow-y-auto overflow-x-hidden px-3 py-3">
						<ul className="space-y-1">
							{groups.map((group, idx) => (
								<li
									key={group.label}
									className="animate-in fade-in slide-in-from-left-2 duration-300"
									style={{
										animationDelay: `${Math.min(idx, 8) * 30}ms`,
										animationFillMode: "both",
									}}
								>
									{group.children ? (
										<GroupRow
											group={group as NavGroup & { children: NavChild[] }}
											compact={compact}
											active={isGroupActive(pathname, group)}
											isOpen={openIndex === idx}
											onToggle={() =>
												setOpenIndex((prev) => (prev === idx ? null : idx))
											}
											onExpandInto={() => expandInto(idx)}
										/>
									) : (
										<LeafRow
											to={group.to}
											icon={group.icon}
											label={group.label}
											compact={compact}
										/>
									)}
								</li>
							))}
						</ul>
					</nav>
				</TooltipProvider>

				<div className="hidden border-t border-sidebar-border/70 p-2 md:block">
					<CollapseToggle compact={compact} />
				</div>
			</aside>
		</>
	);
}

/* ---------------------------------------------------------------- rows --- */

const rowBase =
	"group/row relative flex h-9 items-center rounded-lg text-sm font-medium transition-colors duration-150 outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring";

const SIDEBAR_ICON_SIZE = 18;
const SIDEBAR_CONTROL_ICON_SIZE = 16;

const indicator =
	"before:absolute before:-left-3 before:top-1/2 before:h-0 before:w-1 before:-translate-y-1/2 before:rounded-r-full before:bg-primary before:opacity-0 before:transition-all before:duration-200";

function LeafRow({
	to,
	icon: Icon,
	label,
	compact,
}: {
	to: StaticRoute;
	icon: NavGroup["icon"];
	label: string;
	compact: boolean;
}) {
	const link = (
		<Link
			to={to}
			onClick={closeSidebar}
			activeOptions={{ exact: true }}
			aria-label={compact ? label : undefined}
			className={cn(
				rowBase,
				"text-sidebar-foreground/75 hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
				"data-[status=active]:bg-sidebar-accent data-[status=active]:text-sidebar-accent-foreground",
				compact ? "w-11 justify-center" : "gap-3 px-3",
			)}
		>
			<Icon size={SIDEBAR_ICON_SIZE} className="shrink-0" />
			{!compact && <span className="truncate">{label}</span>}
		</Link>
	);

	if (compact) {
		return (
			<Tooltip>
				<TooltipTrigger render={link} />
				<TooltipContent side="right" sideOffset={10}>
					{label}
				</TooltipContent>
			</Tooltip>
		);
	}
	return link;
}

function GroupRow({
	group,
	compact,
	active,
	isOpen,
	onToggle,
	onExpandInto,
}: {
	group: NavGroup & { children: NavChild[] };
	compact: boolean;
	active: boolean;
	isOpen: boolean;
	onToggle: () => void;
	onExpandInto: () => void;
}) {
	const Icon = group.icon;

	if (compact) {
		const btn = (
			<button
				type="button"
				onClick={onExpandInto}
				aria-label={group.label}
				className={cn(
					rowBase,
					indicator,
					"w-11 justify-center hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
					active
						? "bg-sidebar-accent text-sidebar-accent-foreground before:h-5 before:opacity-100"
						: "text-sidebar-foreground/75",
				)}
			>
				<Icon size={SIDEBAR_ICON_SIZE} className="shrink-0" />
			</button>
		);
		return (
			<Tooltip>
				<TooltipTrigger render={btn} />
				<TooltipContent side="right" sideOffset={10}>
					{group.label}
				</TooltipContent>
			</Tooltip>
		);
	}

	return (
		<>
			<button
				type="button"
				onClick={onToggle}
				aria-expanded={isOpen}
				className={cn(
					rowBase,
					"w-full gap-3 px-3 hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
					active
						? "text-sidebar-accent-foreground"
						: "text-sidebar-foreground/75",
				)}
			>
				<Icon
					size={SIDEBAR_ICON_SIZE}
					className={cn("shrink-0 transition-colors", active && "text-primary")}
				/>
				<span className="flex-1 truncate text-left">{group.label}</span>
				<CaretDown
					size={15}
					className={cn(
						"shrink-0 text-sidebar-foreground/50 transition-transform duration-300",
						isOpen && "rotate-180",
					)}
				/>
			</button>

			<div
				className={cn(
					"grid transition-all duration-300",
					SIDEBAR_EASE,
					isOpen ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0",
				)}
			>
				<div className="overflow-hidden" inert={!isOpen}>
					<ul className="mt-1 ml-[1.4rem] space-y-0.5 border-l border-sidebar-border/70 pl-3">
						{group.children.map((child) => (
							<li key={child.to}>
								<ChildRow child={child} />
							</li>
						))}
					</ul>
				</div>
			</div>
		</>
	);
}

function ChildRow({ child }: { child: NavChild }) {
	const pathname = useRouterState({ select: (s) => s.location.pathname });
	const active = isChildActive(pathname, child);

	return (
		<Link
			to={child.to}
			onClick={closeSidebar}
			activeOptions={child.exact ? { exact: true } : undefined}
			data-active={active ? "" : undefined}
			className={cn(
				"relative flex h-8 items-center rounded-md pl-3 pr-3 text-sm transition-colors duration-150",
				"text-sidebar-foreground/60 hover:bg-sidebar-accent/50 hover:text-sidebar-accent-foreground",
				"data-[status=active]:font-medium data-[status=active]:text-sidebar-accent-foreground",
				"data-[active]:font-medium data-[active]:text-sidebar-accent-foreground",
			)}
		>
			<span className="truncate">{child.label}</span>
		</Link>
	);
}

function CollapseToggle({ compact }: { compact: boolean }) {
	const { t } = useTranslation();
	const label = compact ? t("nav.expand_sidebar") : t("nav.collapse_sidebar");
	return (
		<button
			type="button"
			onClick={toggleSidebarCollapsed}
			aria-label={label}
			className={cn(
				"group/row flex h-9 items-center rounded-lg text-sm font-medium text-sidebar-foreground/60 transition-colors duration-150 hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
				compact ? "w-11 justify-center" : "w-full gap-3 px-3",
			)}
		>
			<CaretDoubleLeft
				size={SIDEBAR_CONTROL_ICON_SIZE}
				className={cn(
					"shrink-0 transition-transform duration-300",
					compact && "rotate-180",
				)}
			/>
			{!compact && <span className="flex-1 truncate text-left">{label}</span>}
		</button>
	);
}
