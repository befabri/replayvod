import { useTranslation } from "react-i18next";
import { useStatistics } from "@/features/videos/queries";

type StatusKey = "DONE" | "RUNNING" | "FAILED";

export function VideoStatistics() {
	const { t } = useTranslation();
	const { data, isLoading, isError } = useStatistics();

	const getCount = (status: StatusKey) =>
		data?.by_status.find((b) => b.status === status)?.count ?? 0;

	return (
		<div className="rounded-lg bg-card text-card-foreground p-4 shadow-sm sm:p-5">
			<h5 className="mb-4 text-xl font-medium text-foreground">
				{t("nav.videos")}
			</h5>
			{isLoading ? (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			) : isError ? (
				<div className="text-destructive">{t("videos.failed_to_load")}</div>
			) : (
				<div className="flex flex-col">
					<Stat value={getCount("DONE")} label={t("videos.status.DONE")} />
					<Stat
						value={getCount("RUNNING")}
						label={t("videos.status.RUNNING")}
						className="mt-2"
					/>
					<Stat
						value={getCount("FAILED")}
						label={t("videos.status.FAILED")}
						className="mt-2"
					/>
				</div>
			)}
		</div>
	);
}

function Stat({
	value,
	label,
	className,
}: {
	value: number;
	label: string;
	className?: string;
}) {
	return (
		<div className={className}>
			<span className="text-3xl font-bold tracking-tight text-foreground">
				{value.toLocaleString()}
			</span>
			<div className="text-xl font-normal text-muted-foreground">{label}</div>
		</div>
	);
}
