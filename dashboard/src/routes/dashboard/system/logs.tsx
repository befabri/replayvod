import { createFileRoute } from "@tanstack/react-router";
import { type ReactNode, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { DataTable } from "@/components/ui/data-table";
import { Pager } from "@/components/ui/pager";
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
import { requireRole } from "@/lib/route-guards";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 50;

type Source = "events" | "api";

export const Route = createFileRoute("/dashboard/system/logs")({
	beforeLoad: requireRole("owner"),
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
				<EventsView
					key={`${domain ?? ""}:${severity ?? ""}`}
					domain={domain}
					severity={severity}
				/>
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
	const { data, isLoading, isPlaceholderData, error } = useEventLogs({
		limit: PAGE_SIZE,
		offset: page * PAGE_SIZE,
		domain,
		severity,
	});
	useLiveSystemEvents();

	const columns = useMemo(() => eventLogColumns(t), [t]);

	return (
		<>
			{error && (
				<Alert variant="destructive">
					{t("events.failed_to_load")}: {error.message}
				</Alert>
			)}
			{(isLoading || data) && (
				<PageFlip busy={isPlaceholderData}>
					<DataTable
						columns={columns}
						data={data?.data ?? []}
						loading={isLoading}
						emptyMessage={t("events.empty")}
					/>
					{data && (
						<Pager
							page={page}
							total={data.total}
							hasNext={(page + 1) * PAGE_SIZE < data.total}
							onPrev={() => setPage((p) => Math.max(0, p - 1))}
							onNext={() => setPage((p) => p + 1)}
						/>
					)}
				</PageFlip>
			)}
		</>
	);
}

function ApiLogsView() {
	const { t } = useTranslation();
	const [page, setPage] = useState(0);
	const { data, isLoading, isPlaceholderData, error } = useFetchLogs(
		PAGE_SIZE,
		page * PAGE_SIZE,
	);
	const columns = useMemo(() => fetchLogColumns(t), [t]);

	return (
		<>
			{error && (
				<Alert variant="destructive">
					{t("logs.api_failed")}: {error.message}
				</Alert>
			)}
			{!error && (
				<PageFlip busy={isPlaceholderData}>
					<DataTable
						columns={columns}
						data={data?.data ?? []}
						loading={isLoading}
						emptyMessage={t("logs.api_empty")}
					/>
					<Pager
						page={page}
						total={data?.total ?? 0}
						hasNext={(page + 1) * PAGE_SIZE < (data?.total ?? 0)}
						onPrev={() => setPage((p) => Math.max(0, p - 1))}
						onNext={() => setPage((p) => p + 1)}
					/>
				</PageFlip>
			)}
		</>
	);
}

function PageFlip({ busy, children }: { busy: boolean; children: ReactNode }) {
	return (
		<div
			aria-busy={busy || undefined}
			className={cn("transition-opacity", busy && "opacity-60")}
		>
			{children}
		</div>
	);
}
