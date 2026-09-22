import { CalendarPlusIcon } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { DocsLink } from "@/components/layout/docs-link";
import { TitledLayout } from "@/components/layout/titled-layout";
import { EmptyState } from "@/components/ui/empty-state";
import type { ScheduleRequestResponse } from "@/features/requests";
import {
	useAllScheduleRequests,
	useMyScheduleRequests,
} from "@/features/requests";
import { ApproveRequestDialog } from "@/features/requests/components/ApproveRequestDialog";
import {
	adminRequestColumns,
	myRequestColumns,
} from "@/features/requests/components/columns";
import { RequestScheduleDialog } from "@/features/requests/components/RequestScheduleDialog";
import { RequestTable } from "@/features/requests/components/RequestTable";
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
	const user = useSelector(authStore, (s) => s.user);
	const canManage = hasRole(user, "admin");
	const cta = canManage ? <CreateScheduleDialog /> : <RequestScheduleDialog />;

	return (
		<TitledLayout
			title={t("schedules.title")}
			description={t("schedules.description")}
			actions={
				<>
					<DocsLink page="schedules/">{t("docs.schedules")}</DocsLink>
					{hasSchedules && (
						<>
							{canManage && <PauseAllButton />}
							{cta}
						</>
					)}
				</>
			}
		>
			<SchedulesPausedBanner />

			{canManage ? <RequestsQueue /> : <MyRequests />}

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
					description={
						canManage ? t("schedules.empty") : t("schedules.empty_viewer")
					}
					action={cta}
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

function RequestsQueue() {
	const { t } = useTranslation();
	const requests = useAllScheduleRequests();
	const [approving, setApproving] = useState<ScheduleRequestResponse | null>(
		null,
	);
	const columns = useMemo(() => adminRequestColumns(t, setApproving), [t]);

	if (!requests.isError && (requests.data?.length ?? 0) === 0) return null;

	return (
		<section className="mb-8">
			<h2 className="mb-3 text-lg font-semibold">
				{t("requests.queue_title")}
			</h2>
			<RequestTable
				query={requests}
				columns={columns}
				emptyMessage={t("requests.empty_queue")}
				errorLabel={t("requests.failed_to_load")}
			/>
			<ApproveRequestDialog
				request={approving}
				onClose={() => setApproving(null)}
			/>
		</section>
	);
}

function MyRequests() {
	const { t } = useTranslation();
	const requests = useMyScheduleRequests();
	const columns = useMemo(() => myRequestColumns(t), [t]);

	return (
		<section className="mb-8">
			<h2 className="mb-3 text-lg font-semibold">{t("requests.mine_title")}</h2>
			<RequestTable
				query={requests}
				columns={columns}
				emptyMessage={t("requests.empty")}
				errorLabel={t("requests.failed_to_load")}
			/>
		</section>
	);
}
