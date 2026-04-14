import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { useSchedules } from "@/features/schedules";
import { CreateForm } from "@/features/schedules/components/CreateForm";
import { ScheduleRow } from "@/features/schedules/components/ScheduleRow";

export const Route = createFileRoute("/dashboard/schedules")({
	component: SchedulesPage,
});

function SchedulesPage() {
	const { t } = useTranslation();
	const { data, isLoading, error } = useSchedules();

	return (
		<TitledLayout title={t("schedules.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("schedules.description")}
			</p>

			<CreateForm />

			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("schedules.failed_to_load")}: {error.message}
				</div>
			)}
			{data && data.data.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground mt-8">{t("schedules.empty")}</div>
			)}

			{data && data.data.length > 0 && (
				<div className="mt-8 grid grid-cols-1 lg:grid-cols-[repeat(auto-fit,minmax(600px,1fr))] gap-4">
					{data.data.map((s) => (
						<ScheduleRow key={s.id} schedule={s} />
					))}
				</div>
			)}
		</TitledLayout>
	);
}
