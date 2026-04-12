import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { useSchedules } from "@/features/schedules"
import { CreateForm } from "@/features/schedules/components/CreateForm"
import { ScheduleRow } from "@/features/schedules/components/ScheduleRow"

export const Route = createFileRoute("/dashboard/schedules")({
	component: SchedulesPage,
})

function SchedulesPage() {
	const { t } = useTranslation()
	const { data, isLoading, error } = useSchedules()

	return (
		<div className="p-8 max-w-4xl">
			<h1 className="text-3xl font-heading font-bold mb-2">
				{t("schedules.title")}
			</h1>
			<p className="text-sm text-muted-foreground mb-6">
				{t("schedules.description")}
			</p>

			<CreateForm />

			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("schedules.failed_to_load")}: {error.message}
				</div>
			)}
			{data && data.data.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground mt-8">
					{t("schedules.empty")}
				</div>
			)}

			{data && data.data.length > 0 && (
				<div className="mt-8 space-y-3">
					{data.data.map((s) => (
						<ScheduleRow key={s.id} schedule={s} />
					))}
				</div>
			)}
		</div>
	)
}
