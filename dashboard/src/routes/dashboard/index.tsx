import { createFileRoute } from "@tanstack/react-router";
import { useStore } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import {
	LastLiveStatistics,
	RunningDownloads,
	ScheduleStatistics,
	VideoStatistics,
} from "@/features/dashboard";
import { EventSubSetupNudge, useEventSubConfig } from "@/features/eventsub";
import { LiveStreamsCard } from "@/features/streams-live/LiveStreamsCard";
import { authStore } from "@/stores/auth";

export const Route = createFileRoute("/dashboard/")({
	component: DashboardHome,
});

function DashboardHome() {
	const { t } = useTranslation();
	const user = useStore(authStore, (state) => state.user);
	const isOwner = user?.role === "owner";
	const eventSubConfig = useEventSubConfig({ enabled: isOwner });
	const eventSub = isOwner ? eventSubConfig.data : undefined;

	return (
		<TitledLayout title={t("nav.dashboard")}>
			{eventSub && (eventSub.setup_required || eventSub.restart_required) && (
				<div className="mb-4">
					<EventSubSetupNudge
						setupRequired={eventSub.setup_required}
						restartRequired={eventSub.restart_required}
					/>
				</div>
			)}
			<div className="mb-4 grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
				<VideoStatistics />
				<LastLiveStatistics />
				<ScheduleStatistics />
			</div>
			<LiveStreamsCard />
			<div className="mt-6">
				<RunningDownloads />
			</div>
		</TitledLayout>
	);
}
