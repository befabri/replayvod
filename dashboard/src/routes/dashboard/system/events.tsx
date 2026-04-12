import { createFileRoute } from "@tanstack/react-router"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { DataTable } from "@/components/ui/data-table"
import { useEventLogs, useLiveSystemEvents } from "@/features/eventlogs"
import { eventLogColumns } from "@/features/eventlogs/components/columns"

const PAGE_SIZE = 50

export const Route = createFileRoute("/dashboard/system/events")({
	validateSearch: (search: Record<string, unknown>) => ({
		domain: typeof search.domain === "string" ? search.domain : undefined,
		severity: typeof search.severity === "string" ? search.severity : undefined,
	}),
	component: EventsPage,
})

function EventsPage() {
	const { t } = useTranslation()
	const { domain, severity } = Route.useSearch()
	const [page, setPage] = useState(0)
	const { data, isLoading, error } = useEventLogs({
		limit: PAGE_SIZE,
		offset: page * PAGE_SIZE,
		domain,
		severity,
	})
	// Mount the system.events SSE subscription — each new row
	// invalidates the current page so operators see activity land in
	// real time without refreshing.
	useLiveSystemEvents()

	const columns = useMemo(() => eventLogColumns(t), [t])

	return (
		<div className="p-8 max-w-5xl">
			<h1 className="text-3xl font-heading font-bold mb-2">
				{t("events.title")}
			</h1>
			<p className="text-sm text-muted-foreground mb-4">
				{t("events.description")}
			</p>

			<Filters domain={domain} severity={severity} />

			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("events.failed_to_load")}: {error.message}
				</div>
			)}

			{data && (
				<>
					<DataTable
						columns={columns}
						data={data.data}
						emptyMessage={t("events.empty")}
					/>
					<div className="flex items-center gap-2 mt-4">
						<button
							type="button"
							disabled={page === 0}
							onClick={() => setPage((p) => Math.max(0, p - 1))}
							className="px-3 py-1 rounded-md border border-border disabled:opacity-50 text-sm"
						>
							{t("events.prev")}
						</button>
						<span className="text-sm text-muted-foreground">
							{t("events.page", { n: page + 1 })} · {data.total}{" "}
							{t("events.total")}
						</span>
						<button
							type="button"
							disabled={(page + 1) * PAGE_SIZE >= data.total}
							onClick={() => setPage((p) => p + 1)}
							className="px-3 py-1 rounded-md border border-border disabled:opacity-50 text-sm"
						>
							{t("events.next")}
						</button>
					</div>
				</>
			)}
		</div>
	)
}

function Filters({
	domain,
	severity,
}: {
	domain?: string
	severity?: string
}) {
	const navigate = Route.useNavigate()
	const { t } = useTranslation()
	return (
		<div className="flex items-center gap-2 mb-4 text-sm">
			<select
				value={domain ?? ""}
				onChange={(e) =>
					void navigate({
						search: (s) => ({ ...s, domain: e.target.value || undefined }),
					})
				}
				className="rounded-md border border-border bg-background px-3 py-1 text-sm"
			>
				<option value="">{t("events.all_domains")}</option>
				<option value="schedule">schedule</option>
				<option value="download">download</option>
				<option value="eventsub">eventsub</option>
				<option value="task">task</option>
				<option value="auth">auth</option>
				<option value="system">system</option>
			</select>
			<select
				value={severity ?? ""}
				onChange={(e) =>
					void navigate({
						search: (s) => ({ ...s, severity: e.target.value || undefined }),
					})
				}
				className="rounded-md border border-border bg-background px-3 py-1 text-sm"
			>
				<option value="">{t("events.all_severities")}</option>
				<option value="debug">debug</option>
				<option value="info">info</option>
				<option value="warn">warn</option>
				<option value="error">error</option>
			</select>
		</div>
	)
}
