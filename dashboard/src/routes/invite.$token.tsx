import { TwitchLogoIcon } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { API_URL } from "@/env";

export const Route = createFileRoute("/invite/$token")({
	component: InvitePage,
});

function InvitePage() {
	const { t } = useTranslation();
	const { token } = Route.useParams();

	return (
		<div className="flex min-h-screen">
			<section className="flex flex-col items-center justify-center w-full md:w-1/2 bg-popover text-popover-foreground p-8">
				<h1 className="text-2xl font-heading font-semibold mb-2">
					{t("invite.title")}
				</h1>
				<p className="mb-6 max-w-sm text-center">{t("invite.subtitle")}</p>

				<a
					href={`${API_URL}/api/v1/auth/twitch?invite=${encodeURIComponent(token)}`}
					className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-5 py-2.5 text-foreground font-medium hover:bg-primary-hover transition-colors duration-75"
				>
					<TwitchLogoIcon weight="fill" size={18} />
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
