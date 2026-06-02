import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
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
import { useFetchLogs } from "@/features/system";
import { fetchLogColumns } from "@/features/system/components/logColumns";

const PAGE_SIZE = 50;

type Source = "events" | "api";

export const Route = createFileRoute("/dashboard/system/logs")({
	component: LogsPage,
});

const pick = (set: (v: string | undefined) => void) => (value: unknown) =>
	set(typeof value === "string" && value ? value : undefined);

function LogsPage() {
	const { t } = useTranslation();
	const [source, setSource] = useState<Source>("events");
	const [domain, setDomain] = useState<string | undefined>();
	const [severity, setSeverity] = useState<string | undefined>();

	return (
		<TitledLayout title={t("logs.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("logs.description")}
			</p>

			<div className="mb-4 flex flex-wrap items-center gap-2">
				<span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
					{t("logs.source")}
				</span>
				<Select
					value={source}
					onValueChange={(value) =>
						setSource(value === "api" ? "api" : "events")
					}
				>
					<SelectTrigger variant="chip" className="min-w-40">
						<SelectValue>
							{(value: unknown) =>
								value === "api" ? t("logs.source_api") : t("logs.source_events")
							}
						</SelectValue>
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="events">{t("logs.source_events")}</SelectItem>
						<SelectItem value="api">{t("logs.source_api")}</SelectItem>
					</SelectContent>
				</Select>

				{source === "events" && (
					<>
						<span aria-hidden className="mx-1 h-5 w-px bg-border" />
						<Select value={domain ?? ""} onValueChange={pick(setDomain)}>
							<SelectTrigger variant="chip" className="min-w-36">
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
						<Select value={severity ?? ""} onValueChange={pick(setSeverity)}>
							<SelectTrigger variant="chip" className="min-w-36">
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
					</>
				)}
			</div>

			{source === "events" ? (
				<EventsView domain={domain} severity={severity} />
			) : (
				<ApiLogsView />
			)}
		</TitledLayout>
	);
}

function EventsView({
	domain,
	severity,
}: {
	domain?: string;
	severity?: string;
}) {
	const { t } = useTranslation();
	const [page, setPage] = useState(0);
	const { data, isLoading, error } = useEventLogs({
		limit: PAGE_SIZE,
		offset: page * PAGE_SIZE,
		domain,
		severity,
	});
	// Land new rows live while this view is mounted.
	useLiveSystemEvents();

	// Reset to the first page whenever the filters change.
	// biome-ignore lint/correctness/useExhaustiveDependencies: reset on filter change, not page
	useEffect(() => setPage(0), [domain, severity]);

	const columns = useMemo(() => eventLogColumns(t), [t]);

	return (
		<>
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
					<Pager page={page} total={data.total} onPage={setPage} />
				</>
			)}
		</>
	);
}

function ApiLogsView() {
	const { t } = useTranslation();
	const [page, setPage] = useState(0);
	const { data, isLoading, error } = useFetchLogs(PAGE_SIZE, page * PAGE_SIZE);
	const columns = useMemo(() => fetchLogColumns(t), [t]);

	return (
		<>
			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("logs.api_failed")}: {error.message}
				</div>
			)}
			{!isLoading && !error && (
				<>
					<DataTable
						columns={columns}
						data={data?.data ?? []}
						emptyMessage={t("logs.api_empty")}
					/>
					<Pager page={page} total={data?.total ?? 0} onPage={setPage} />
				</>
			)}
		</>
	);
}

function Pager({
	page,
	total,
	onPage,
}: {
	page: number;
	total: number;
	onPage: (updater: (p: number) => number) => void;
}) {
	const { t } = useTranslation();
	const hasNext = (page + 1) * PAGE_SIZE < total;
	return (
		<div className="flex items-center gap-2 mt-4">
			<button
				type="button"
				disabled={page === 0}
				onClick={() => onPage((p) => Math.max(0, p - 1))}
				className="px-3 py-1 rounded-md border border-border disabled:opacity-50 text-sm"
			>
				{t("events.prev")}
			</button>
			<span className="text-sm text-muted-foreground">
				{t("events.page", { n: page + 1 })} · {total} {t("events.total")}
			</span>
			<button
				type="button"
				disabled={!hasNext}
				onClick={() => onPage((p) => p + 1)}
				className="px-3 py-1 rounded-md border border-border disabled:opacity-50 text-sm"
			>
				{t("events.next")}
			</button>
		</div>
	);
}
