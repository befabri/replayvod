import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { VideoStatus } from "@/api/generated/trpc";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useStatistics } from "@/features/videos/queries";

type StatusKey = Extract<VideoStatus, "DONE" | "RUNNING" | "FAILED">;

const STATUSES: readonly StatusKey[] = ["DONE", "RUNNING", "FAILED"];

export function VideoStatistics() {
	const { t } = useTranslation();
	const { data, isLoading, isError, isFetching, refetch } = useStatistics();

	const getCount = (status: StatusKey) =>
		data?.by_status.find((b) => b.status === status)?.count ?? 0;

	return (
		<div className="rounded-lg bg-card text-card-foreground p-4 shadow-sm sm:p-5">
			<h5 className="mb-4 text-xl font-medium text-foreground">
				{t("nav.videos")}
			</h5>
			{isLoading ? (
				<LoadingState className="flex flex-col gap-2">
					{STATUSES.map((status) => (
						<Stat
							key={status}
							value={
								<Skeleton className="inline-block h-7 w-16 align-middle" />
							}
							label={t(`videos.status.${status}`)}
						/>
					))}
				</LoadingState>
			) : isError ? (
				<Alert
					variant="destructive"
					action={
						<Button
							variant="outline"
							size="sm"
							disabled={isFetching}
							onClick={() => void refetch()}
						>
							{t("common.retry")}
						</Button>
					}
				>
					{t("videos.failed_to_load")}
				</Alert>
			) : (
				<div className="flex flex-col gap-2">
					{STATUSES.map((status) => (
						<Stat
							key={status}
							value={getCount(status).toLocaleString()}
							label={t(`videos.status.${status}`)}
						/>
					))}
				</div>
			)}
		</div>
	);
}

function Stat({ value, label }: { value: ReactNode; label: string }) {
	return (
		<div>
			<span className="text-3xl font-bold tracking-tight text-foreground">
				{value}
			</span>
			<div className="text-xl font-normal text-muted-foreground">{label}</div>
		</div>
	);
}
