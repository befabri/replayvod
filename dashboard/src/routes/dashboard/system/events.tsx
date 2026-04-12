import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useEventLogs } from "@/features/eventlogs"

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
			{data && data.data.length === 0 && (
				<div className="text-muted-foreground">{t("events.empty")}</div>
			)}

			{data && data.data.length > 0 && (
				<>
					<div className="rounded-lg border border-border overflow-hidden">
						<table className="w-full text-sm">
							<thead className="bg-muted/50">
								<tr>
									<th className="text-left px-3 py-2 font-medium w-40">
										{t("events.col_time")}
									</th>
									<th className="text-left px-3 py-2 font-medium w-24">
										{t("events.col_severity")}
									</th>
									<th className="text-left px-3 py-2 font-medium w-32">
										{t("events.col_domain")}
									</th>
									<th className="text-left px-3 py-2 font-medium">
										{t("events.col_message")}
									</th>
								</tr>
							</thead>
							<tbody>
								{data.data.map((row) => (
									<tr key={row.id} className="border-t border-border align-top">
										<td className="px-3 py-2 text-xs text-muted-foreground">
											{new Date(row.created_at).toLocaleString()}
										</td>
										<td className="px-3 py-2">
											<SeverityBadge severity={row.severity} />
										</td>
										<td className="px-3 py-2 font-mono text-xs">
											{row.domain}.{row.event_type}
										</td>
										<td className="px-3 py-2">
											<div>{row.message}</div>
											{row.data !== undefined && row.data !== null && (
												<pre className="text-xs text-muted-foreground mt-1 overflow-x-auto">
													{JSON.stringify(row.data as unknown, null, 2)}
												</pre>
											)}
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
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
							{t("events.page", { n: page + 1 })} · {data.total} {t("events.total")}
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

function SeverityBadge({ severity }: { severity: string }) {
	const cls = {
		debug: "bg-muted text-muted-foreground",
		info: "bg-muted text-foreground",
		warn: "bg-yellow-500/20 text-yellow-200",
		error: "bg-destructive/20 text-destructive",
	}[severity] ?? "bg-muted text-muted-foreground"
	return (
		<span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${cls}`}>
			{severity}
		</span>
	)
}
