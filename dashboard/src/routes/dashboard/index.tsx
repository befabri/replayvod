import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import {
	LastLiveStatistics,
	ScheduleStatistics,
	VideoStatistics,
} from "@/features/dashboard";
import { LiveStreamsCard } from "@/features/streams-live/LiveStreamsCard";

export const Route = createFileRoute("/dashboard/")({
	component: DashboardHome,
});

function DashboardHome() {
	const { t } = useTranslation();

	return (
		<TitledLayout title={t("nav.dashboard")}>
			<div className="mb-4 grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
				<VideoStatistics />
				<LastLiveStatistics />
				<ScheduleStatistics />
			</div>
			<LiveStreamsCard />
		</TitledLayout>
	);
}
