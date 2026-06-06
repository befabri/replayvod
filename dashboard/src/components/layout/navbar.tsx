import { ListIcon } from "@phosphor-icons/react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import i18n from "i18next";
import { useTranslation } from "react-i18next";
import {
	GlobalSearch,
	GlobalSearchDialog,
} from "@/components/layout/global-search";
import { Avatar } from "@/components/ui/avatar";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuSubmenu,
	DropdownMenuSubmenuTrigger,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Switch } from "@/components/ui/switch";
import { authStore, logout } from "@/stores/auth";
import { setTheme, themeStore } from "@/stores/theme";
import { openSidebar } from "@/stores/ui";

export function Navbar() {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const user = useSelector(authStore, (s) => s.user);
	const theme = useSelector(themeStore, (s) => s.theme);

	return (
		<nav className="fixed top-0 left-0 right-0 z-50 h-16 bg-navbar text-navbar-foreground border-b border-border shadow-sm">
			<div className="grid h-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 px-3 lg:px-5">
				<div className="flex min-w-0 items-center gap-2">
					<button
						type="button"
						onClick={openSidebar}
						aria-label={t("nav.open_menu")}
						className="md:hidden inline-flex items-center rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
					>
						<ListIcon size={20} weight="bold" />
					</button>
					<Link
						to="/dashboard"
						aria-label={t("app.name")}
						className="hidden min-w-0 items-baseline gap-2 self-center whitespace-nowrap text-[1.1875rem] tracking-[-0.04em] text-navbar-foreground no-underline min-[380px]:inline-flex"
					>
						<span className="inline-flex items-baseline">
							<span className="font-semibold">Replay</span>
							<span className="font-bold text-primary">VOD</span>
							<span className="font-bold text-primary">.</span>
						</span>
					</Link>
				</div>

				<GlobalSearch
					shortcut
					className="mx-auto hidden w-full max-w-xl sm:block"
				/>

				<div className="flex items-center justify-end gap-1">
					<GlobalSearchDialog className="sm:hidden" />
					{user && (
						<DropdownMenu>
							<DropdownMenuTrigger
								aria-label={t("nav.profile_menu")}
								className="flex rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring"
							>
								<Avatar
									src={user.profileImageUrl}
									name={user.displayName}
									alt={user.displayName}
									size="md"
								/>
							</DropdownMenuTrigger>
							<DropdownMenuContent>
								<DropdownMenuLabel className="truncate">
									{user.displayName}
								</DropdownMenuLabel>
								<DropdownMenuSeparator />
								<DropdownMenuSubmenu>
									<DropdownMenuSubmenuTrigger>
										{t("nav.language")}
									</DropdownMenuSubmenuTrigger>
									<DropdownMenuContent align="start">
										<DropdownMenuItem onClick={() => i18n.changeLanguage("en")}>
											English
										</DropdownMenuItem>
										<DropdownMenuItem onClick={() => i18n.changeLanguage("fr")}>
											Français
										</DropdownMenuItem>
									</DropdownMenuContent>
								</DropdownMenuSubmenu>
								<DropdownMenuItem
									closeOnClick={false}
									onClick={(e) => e.preventDefault()}
									className="justify-between"
								>
									<span>{t("nav.theme_dark")}</span>
									<Switch
										checked={theme === "dark"}
										onCheckedChange={(checked) =>
											setTheme(checked ? "dark" : "light")
										}
										aria-label={t("nav.toggle_theme")}
									/>
								</DropdownMenuItem>
								<DropdownMenuSeparator />
								<DropdownMenuItem
									onClick={() => void navigate({ to: "/dashboard/settings" })}
								>
									{t("nav.settings")}
								</DropdownMenuItem>
								<DropdownMenuItem
									onClick={() => void navigate({ to: "/dashboard/sessions" })}
								>
									{t("nav.sessions")}
								</DropdownMenuItem>
								<DropdownMenuItem
									onClick={async () => {
										// logout() clears the auth store, but the route guards only
										// re-run on navigation (beforeLoad), so the redirect has to be
										// driven explicitly here. Without it the cleared store just
										// hides the menu while the dashboard stays mounted.
										await logout();
										await navigate({
											to: "/login",
											search: { error: undefined },
										});
									}}
								>
									{t("auth.logout")}
								</DropdownMenuItem>
							</DropdownMenuContent>
						</DropdownMenu>
					)}
				</div>
			</div>
		</nav>
	);
}
