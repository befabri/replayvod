import { TwitchLogo } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { API_URL } from "@/env";

export const Route = createFileRoute("/login")({
	validateSearch: (search: Record<string, unknown>) => ({
		error: typeof search.error === "string" ? search.error : undefined,
	}),
	component: LoginPage,
});

const ERROR_KEYS = new Set([
	"not_whitelisted",
	"access_denied",
	"invalid_state",
	"invalid_pkce",
]);

// Login matches v1: two-column 50/50 color split, all content centered
// in the left column; right column is pure color with no content.
// bg-popover = #262444 (v1 custom_space_cadet)
// bg-card    = #1C1A31 (v1 custom_lightblue)
function LoginPage() {
	const { t } = useTranslation();
	const { error } = Route.useSearch();

	const errorMessage = error
		? ERROR_KEYS.has(error)
			? t(`auth.error_${error}`)
			: t("auth.error_generic", { code: error })
		: null;

	return (
		<div className="flex min-h-screen">
			<section className="flex flex-col items-center justify-center w-full md:w-1/2 bg-popover text-popover-foreground p-8">
				<h1 className="text-2xl font-heading font-semibold mb-6">
					{t("auth.sign_in_title")}
				</h1>

				{errorMessage && (
					<div
						role="alert"
						className="mb-4 w-full max-w-sm rounded-md bg-destructive/10 p-3 text-destructive text-sm"
					>
						{errorMessage}
					</div>
				)}

				<a
					href={`${API_URL}/api/v1/auth/twitch`}
					className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-5 py-2.5 text-foreground font-medium hover:bg-primary-hover transition-colors duration-75"
				>
					<TwitchLogo weight="fill" size={18} />
					{t("auth.twitch_connect")}
				</a>
			</section>
			<section
				aria-hidden="true"
				className="hidden md:block md:w-1/2 bg-card"
			/>
		</div>
	);
}
