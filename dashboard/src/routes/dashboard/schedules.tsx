import { CalendarPlusIcon } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { DocsLink } from "@/components/layout/docs-link";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { DataTable } from "@/components/ui/data-table";
import { EmptyState } from "@/components/ui/empty-state";
import { LoadingState } from "@/components/ui/loading-state";
import { ButtonSkeleton } from "@/components/ui/skeleton";
import {
	useAllScheduleRequests,
	useMyScheduleRequests,
} from "@/features/requests";
import { myRequestColumns } from "@/features/requests/components/columns";
import { MyRequests } from "@/features/requests/components/MyRequests";
import { RequestScheduleDialog } from "@/features/requests/components/RequestScheduleDialog";
import { RequestsQueue } from "@/features/requests/components/RequestsQueue";
import { useSchedules, useSchedulesPaused } from "@/features/schedules";
import { CreateScheduleDialog } from "@/features/schedules/components/CreateScheduleDialog";
import { PauseAllButton } from "@/features/schedules/components/PauseAllButton";
import {
	ScheduleRow,
	ScheduleRowSkeleton,
} from "@/features/schedules/components/ScheduleRow";
import { SchedulesPausedBanner } from "@/features/schedules/components/SchedulesPausedBanner";
import { authStore, hasRole } from "@/stores/auth";

const SCHEDULE_GRID_CLASS =
	"grid grid-cols-1 lg:grid-cols-[repeat(auto-fit,minmax(600px,1fr))] gap-4";

const SKELETON_ROWS = ["schedule-1", "schedule-2", "schedule-3", "schedule-4"];

export const Route = createFileRoute("/dashboard/schedules")({
	component: SchedulesPage,
});

function SchedulesPage() {
	const { t } = useTranslation();
	const user = useSelector(authStore, (s) => s.user);
	const canManage = hasRole(user, "admin");
	const { data, isPending, error } = useSchedules();
	const pauseState = useSchedulesPaused();
	const queue = useAllScheduleRequests({ enabled: canManage });
	const mine = useMyScheduleRequests({ enabled: !canManage });
	const requests = canManage ? queue : mine;
	const pending = isPending || pauseState.isPending || requests.isPending;
	const [settled, setSettled] = useState(false);
	if (!pending && !settled) setSettled(true);
	const loading = !settled;
	const hasSchedules = (data?.data.length ?? 0) > 0;
	const globallyPaused = pauseState.data?.paused ?? false;
	const cta = canManage ? <CreateScheduleDialog /> : <RequestScheduleDialog />;

	return (
		<TitledLayout
			title={t("schedules.title")}
			description={t("schedules.description")}
			actions={
				<>
					<DocsLink page="schedules/">{t("docs.schedules")}</DocsLink>
					{loading ? (
						<SchedulesActionsSkeleton canManage={canManage} />
					) : (
						hasSchedules && (
							<>
								{canManage && <PauseAllButton />}
								{cta}
							</>
						)
					)}
				</>
			}
		>
			{loading ? (
				<SchedulesSkeleton canManage={canManage} />
			) : (
				<>
					<SchedulesPausedBanner />

					{canManage ? (
						<RequestsQueue requests={queue} />
					) : (
						<MyRequests requests={mine} />
					)}

					{error && (
						<Alert variant="destructive">
							{t("schedules.failed_to_load")}: {error.message}
						</Alert>
					)}
					{data && data.data.length === 0 && (
						<EmptyState
							icon={<CalendarPlusIcon weight="duotone" />}
							title={t("schedules.empty_title")}
							description={
								canManage ? t("schedules.empty") : t("schedules.empty_viewer")
							}
							action={cta}
						/>
					)}

					{data && data.data.length > 0 && (
						<div className={SCHEDULE_GRID_CLASS}>
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
				</>
			)}
		</TitledLayout>
	);
}

function SchedulesActionsSkeleton({ canManage }: { canManage: boolean }) {
	return canManage ? (
		<>
			<ButtonSkeleton className="w-26" />
			<ButtonSkeleton className="w-34" />
		</>
	) : (
		<ButtonSkeleton className="w-36" />
	);
}

function SchedulesSkeleton({ canManage }: { canManage: boolean }) {
	const { t } = useTranslation();
	const columns = useMemo(() => myRequestColumns(t), [t]);
	return (
		<LoadingState>
			{canManage ? null : (
				<section className="mb-8">
					<h2 className="mb-3 text-lg font-semibold">
						{t("requests.mine_title")}
					</h2>
					<DataTable
						columns={columns}
						data={[]}
						emptyMessage={t("requests.empty")}
						loading
						loadingRows={0}
					/>
				</section>
			)}
			<div className={SCHEDULE_GRID_CLASS}>
				{SKELETON_ROWS.map((key) => (
					<ScheduleRowSkeleton key={key} canManage={canManage} />
				))}
			</div>
		</LoadingState>
	);
}
