import { List } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useStore } from "@tanstack/react-store";
import i18n from "i18next";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
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
	const user = useStore(authStore, (s) => s.user);
	const theme = useStore(themeStore, (s) => s.theme);

	return (
		<nav className="fixed top-0 left-0 right-0 z-50 h-16 bg-navbar text-navbar-foreground border-b border-border shadow-sm">
			<div className="h-full px-3 lg:px-5 flex items-center justify-between">
				<div className="flex items-center gap-2">
					<button
						type="button"
						onClick={openSidebar}
						aria-label={t("nav.open_menu")}
						className="md:hidden inline-flex items-center rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
					>
						<List size={20} weight="bold" />
					</button>
					<Link
						to="/dashboard"
						className="self-center whitespace-nowrap text-xl md:text-2xl font-heading font-semibold text-navbar-foreground"
					>
						{t("app.name")}
					</Link>
				</div>

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
							<DropdownMenuItem onClick={() => void logout()}>
								{t("auth.logout")}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				)}
			</div>
		</nav>
	);
}
