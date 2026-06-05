import { CalendarPlusIcon } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { EmptyState } from "@/components/ui/empty-state";
import { useSchedules, useSchedulesPaused } from "@/features/schedules";
import { CreateScheduleDialog } from "@/features/schedules/components/CreateScheduleDialog";
import { PauseAllButton } from "@/features/schedules/components/PauseAllButton";
import { ScheduleRow } from "@/features/schedules/components/ScheduleRow";
import { SchedulesPausedBanner } from "@/features/schedules/components/SchedulesPausedBanner";
import { authStore, hasRole } from "@/stores/auth";

export const Route = createFileRoute("/dashboard/schedules")({
	component: SchedulesPage,
});

function SchedulesPage() {
	const { t } = useTranslation();
	const { data, isLoading, error } = useSchedules();
	const { data: pauseState } = useSchedulesPaused();
	const hasSchedules = (data?.data.length ?? 0) > 0;
	const globallyPaused = pauseState?.paused ?? false;
	// schedule.setPaused and schedule.create are admin-only on the server, so the
	// management controls are hidden from viewers rather than letting them act
	// and hit a 403. Viewers still see the list read-only.
	const user = useSelector(authStore, (s) => s.user);
	const canManage = hasRole(user, "admin");

	return (
		<TitledLayout
			title={t("schedules.title")}
			description={t("schedules.description")}
			actions={
				// Header controls only when there's something to manage. On the empty
				// page the EmptyState carries the sole create CTA (no duplicate).
				canManage && hasSchedules ? (
					<>
						<PauseAllButton />
						<CreateScheduleDialog />
					</>
				) : null
			}
		>
			<SchedulesPausedBanner />

			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("schedules.failed_to_load")}: {error.message}
				</div>
			)}
			{data && data.data.length === 0 && !isLoading && !error && (
				<EmptyState
					icon={<CalendarPlusIcon weight="duotone" />}
					title={t("schedules.empty_title")}
					description={t("schedules.empty")}
					action={canManage ? <CreateScheduleDialog /> : undefined}
				/>
			)}

			{data && data.data.length > 0 && (
				<div className="grid grid-cols-1 lg:grid-cols-[repeat(auto-fit,minmax(600px,1fr))] gap-4">
					{data.data.map((s) => (
						<ScheduleRow
							key={s.id}
							schedule={s}
							globallyPaused={globallyPaused}
							canManage={canManage}
						/>
					))}
				</div>
			)}
		</TitledLayout>
	);
}
