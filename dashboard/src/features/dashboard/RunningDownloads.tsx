import { useTranslation } from "react-i18next";
import { useLiveActiveDownloads } from "@/features/videos";
import { formatBytes } from "@/features/videos/format";

export function RunningDownloads() {
	const { t } = useTranslation();
	const { data, isLoading, isError, error } = useLiveActiveDownloads();
	const rows = data ?? [];

	return (
		<section className="rounded-2xl border border-border bg-card/70 px-4 py-4 sm:px-6 sm:py-5">
			<div className="flex items-center justify-between gap-4 border-b border-border pb-5">
				<h2 className="text-[1.35rem] font-medium tracking-tight text-foreground">
					{t("dashboard.running_now")}
				</h2>
				<div className="font-mono text-xs tracking-[0.16em] text-muted-foreground uppercase">
					{t("dashboard.active_count", { count: rows.length })}
				</div>
			</div>

			{isLoading ? (
				<div className="pt-6 text-sm text-muted-foreground">
					{t("common.loading")}
				</div>
			) : isError && error ? (
				<div className="pt-6 text-sm text-destructive">
					{t("dashboard.running_now_failed")}: {error.message}
				</div>
			) : rows.length === 0 ? (
				<div className="pt-6 text-sm text-muted-foreground">
					{t("dashboard.running_now_empty")}
				</div>
			) : (
				<div>
					{rows.map((row) => {
						const progressWidth =
							row.percent > 0 ? `${Math.max(row.percent, 3)}%` : "3%";
						const estimatedBytes = estimateTotalBytes(
							row.bytes_written,
							row.percent,
						);

						return (
							<div
								key={row.video.job_id}
								className="grid gap-4 border-border py-6 [&:not(:first-child)]:border-t md:grid-cols-[280px_minmax(0,1fr)_120px_140px] md:items-center md:gap-8"
							>
								<div className="min-w-0">
									<div className="truncate text-[1.05rem] font-medium text-foreground">
										{row.video.broadcaster_name || row.video.display_name}
										<span className="px-2 text-muted-foreground">·</span>
										<span className="lowercase text-foreground/90">
											{row.stage}
										</span>
									</div>
									<div className="mt-2 flex flex-wrap items-center gap-2 font-mono text-sm text-muted-foreground">
										<span>{formatBytes(row.bytes_written)}</span>
										{estimatedBytes ? (
											<>
												<span>/</span>
												<span>{formatBytes(estimatedBytes)}</span>
											</>
										) : null}
										<span>·</span>
										<span className="text-foreground/55">
											{row.video.quality}
										</span>
									</div>
								</div>

								<div className="h-1.5 overflow-hidden rounded-full bg-muted/55">
									<div
										className="h-full rounded-full bg-primary/90 transition-[width] duration-500 ease-out"
										style={{ width: progressWidth }}
									/>
								</div>

								<div className="text-right font-mono text-sm text-muted-foreground md:justify-self-end">
									{row.speed || "-"}
								</div>

								<div className="text-right font-mono text-sm text-muted-foreground md:justify-self-end">
									{row.eta ? `${row.eta} left` : "-"}
								</div>
							</div>
						);
					})}
				</div>
			)}
		</section>
	);
}

function estimateTotalBytes(bytesWritten: number, percent: number) {
	if (bytesWritten <= 0 || percent <= 0 || percent > 100) return null;
	return Math.round(bytesWritten / (percent / 100));
}
