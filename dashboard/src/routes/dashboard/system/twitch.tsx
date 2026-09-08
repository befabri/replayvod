import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { TwitchPlaybackCard } from "@/features/system/components/TwitchPlaybackCard";
import { requireRole } from "@/lib/route-guards";

export const Route = createFileRoute("/dashboard/system/twitch")({
	beforeLoad: requireRole("owner"),
	component: TwitchPlaybackPage,
});

function TwitchPlaybackPage() {
	const { t } = useTranslation();
	return (
		<TitledLayout title={t("twitch_playback.title")}>
			<TwitchPlaybackCard />
		</TitledLayout>
	);
}
