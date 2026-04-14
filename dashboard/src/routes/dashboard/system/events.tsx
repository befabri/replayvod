import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { useEventLogs, useLiveSystemEvents } from "@/features/eventlogs";
import { eventLogColumns } from "@/features/eventlogs/components/columns";

const PAGE_SIZE = 50;

export const Route = createFileRoute("/dashboard/system/events")({
	validateSearch: (search: Record<string, unknown>) => ({
		domain: typeof search.domain === "string" ? search.domain : undefined,
		severity: typeof search.severity === "string" ? search.severity : undefined,
	}),
	component: EventsPage,
});

function EventsPage() {
	const { t } = useTranslation();
	const { domain, severity } = Route.useSearch();
	const [page, setPage] = useState(0);
	const { data, isLoading, error } = useEventLogs({
		limit: PAGE_SIZE,
		offset: page * PAGE_SIZE,
		domain,
		severity,
	});
	// Mount the system.events SSE subscription — each new row
	// invalidates the current page so operators see activity land in
	// real time without refreshing.
	useLiveSystemEvents();

	const columns = useMemo(() => eventLogColumns(t), [t]);

	return (
		<TitledLayout title={t("events.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
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
		</TitledLayout>
	);
}

function Filters({ domain, severity }: { domain?: string; severity?: string }) {
	const navigate = Route.useNavigate();
	const { t } = useTranslation();
	return (
		<div className="flex items-center gap-2 mb-4 text-sm">
			<Select
				value={domain ?? ""}
				onValueChange={(value) =>
					void navigate({
						search: (s) => ({
							...s,
							domain: typeof value === "string" && value ? value : undefined,
						}),
					})
				}
			>
				<SelectTrigger className="w-40">
					<SelectValue placeholder={t("events.all_domains")} />
				</SelectTrigger>
				<SelectContent>
					<SelectItem value="">{t("events.all_domains")}</SelectItem>
					<SelectItem value="schedule">schedule</SelectItem>
					<SelectItem value="download">download</SelectItem>
					<SelectItem value="eventsub">eventsub</SelectItem>
					<SelectItem value="task">task</SelectItem>
					<SelectItem value="auth">auth</SelectItem>
					<SelectItem value="system">system</SelectItem>
				</SelectContent>
			</Select>
			<Select
				value={severity ?? ""}
				onValueChange={(value) =>
					void navigate({
						search: (s) => ({
							...s,
							severity: typeof value === "string" && value ? value : undefined,
						}),
					})
				}
			>
				<SelectTrigger className="w-40">
					<SelectValue placeholder={t("events.all_severities")} />
				</SelectTrigger>
				<SelectContent>
					<SelectItem value="">{t("events.all_severities")}</SelectItem>
					<SelectItem value="debug">debug</SelectItem>
					<SelectItem value="info">info</SelectItem>
					<SelectItem value="warn">warn</SelectItem>
					<SelectItem value="error">error</SelectItem>
				</SelectContent>
			</Select>
		</div>
	);
}
