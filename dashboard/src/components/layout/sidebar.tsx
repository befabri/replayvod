import { Link } from "@tanstack/react-router"
import { useStore } from "@tanstack/react-store"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { authStore, hasRole, logout } from "@/stores/auth"

export function Sidebar() {
	const { t } = useTranslation()
	const user = useStore(authStore, (s) => s.user)
	const [open, setOpen] = useState(false)

	// Close the drawer on Escape for keyboard parity with the backdrop click.
	useEffect(() => {
		if (!open) return
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") setOpen(false)
		}
		window.addEventListener("keydown", onKey)
		return () => window.removeEventListener("keydown", onKey)
	}, [open])

	return (
		<>
			{/* Hamburger — mobile only */}
			<button
				type="button"
				onClick={() => setOpen(true)}
				aria-label={t("nav.open_menu")}
				className="md:hidden fixed top-3 left-3 z-40 rounded-md bg-sidebar text-sidebar-foreground border border-sidebar-border p-2 shadow-sm"
			>
				<HamburgerIcon />
			</button>

			{/* Backdrop — mobile only */}
			{open && (
				<button
					type="button"
					aria-label={t("nav.close_menu")}
					onClick={() => setOpen(false)}
					className="md:hidden fixed inset-0 z-40 bg-black/40"
				/>
			)}

			{/* Sidebar: fixed on md+, slide-in drawer below md */}
			<aside
				className={`fixed left-0 top-0 bottom-0 w-56 bg-sidebar text-sidebar-foreground border-r border-sidebar-border flex flex-col z-50 transition-transform md:translate-x-0 ${
					open ? "translate-x-0" : "-translate-x-full md:translate-x-0"
				}`}
			>
				<div className="p-4 border-b border-sidebar-border">
					<Link
						to="/dashboard"
						className="text-xl font-heading font-bold"
						onClick={() => setOpen(false)}
					>
						{t("app.name")}
					</Link>
				</div>

				<nav className="flex-1 p-2 space-y-1 overflow-y-auto">
					<NavLink
						to="/dashboard"
						label={t("nav.dashboard")}
						onNavigate={() => setOpen(false)}
					/>
					<NavLink
						to="/dashboard/videos"
						label={t("nav.videos")}
						onNavigate={() => setOpen(false)}
					/>
					<NavLink
						to="/dashboard/channels"
						label={t("nav.channels")}
						onNavigate={() => setOpen(false)}
					/>
					<NavLink
						to="/dashboard/categories"
						label={t("nav.categories")}
						onNavigate={() => setOpen(false)}
					/>
					<NavLink
						to="/dashboard/requests"
						label={t("nav.requests")}
						onNavigate={() => setOpen(false)}
					/>
					<NavLink
						to="/dashboard/schedules"
						label={t("nav.schedules")}
						onNavigate={() => setOpen(false)}
					/>
					{user && hasRole(user, "owner") && (
						<>
							<div className="pt-4 pb-1 px-3 text-xs uppercase tracking-wide text-muted-foreground">
								{t("nav.system")}
							</div>
							<NavLink
								to="/dashboard/system/logs"
								label={t("nav.logs")}
								onNavigate={() => setOpen(false)}
							/>
							<NavLink
								to="/dashboard/system/users"
								label={t("nav.users")}
								onNavigate={() => setOpen(false)}
							/>
							<NavLink
								to="/dashboard/system/whitelist"
								label={t("nav.whitelist")}
								onNavigate={() => setOpen(false)}
							/>
							<NavLink
								to="/dashboard/system/eventsub"
								label={t("nav.eventsub")}
								onNavigate={() => setOpen(false)}
							/>
						</>
					)}
				</nav>

				{user && (
					<div className="p-4 border-t border-sidebar-border">
						<div className="flex items-center gap-2 mb-2">
							{user.profileImageUrl && (
								<img
									src={user.profileImageUrl}
									alt=""
									className="w-8 h-8 rounded-full"
								/>
							)}
							<div className="flex-1 min-w-0">
								<div className="truncate text-sm font-medium">
									{user.displayName}
								</div>
								<div className="text-xs text-muted-foreground capitalize">
									{user.role}
								</div>
							</div>
						</div>
						<button
							type="button"
							onClick={() => logout()}
							className="w-full text-left text-sm text-muted-foreground hover:text-foreground transition-colors"
						>
							{t("auth.logout")}
						</button>
					</div>
				)}
			</aside>
		</>
	)
}

function NavLink({
	to,
	label,
	onNavigate,
}: {
	to: string
	label: string
	onNavigate?: () => void
}) {
	return (
		<Link
			// biome-ignore lint/suspicious/noExplicitAny: TanStack Router types don't narrow well on arbitrary string paths
			to={to as any}
			onClick={onNavigate}
			className="block px-3 py-2 rounded-md text-sm hover:bg-sidebar-accent hover:text-sidebar-accent-foreground transition-colors"
			activeProps={{
				className:
					"block px-3 py-2 rounded-md text-sm bg-sidebar-accent text-sidebar-accent-foreground font-medium",
			}}
		>
			{label}
		</Link>
	)
}

function HamburgerIcon() {
	return (
		<svg
			xmlns="http://www.w3.org/2000/svg"
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			strokeWidth="2"
			strokeLinecap="round"
			strokeLinejoin="round"
			className="w-5 h-5"
			aria-hidden="true"
		>
			<line x1="3" y1="6" x2="21" y2="6" />
			<line x1="3" y1="12" x2="21" y2="12" />
			<line x1="3" y1="18" x2="21" y2="18" />
		</svg>
	)
}
