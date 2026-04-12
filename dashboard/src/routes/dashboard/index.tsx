import { createFileRoute } from "@tanstack/react-router"
import { useStore } from "@tanstack/react-store"
import { useTranslation } from "react-i18next"
import { LiveStreamsCard } from "@/features/streams-live"
import { useStatistics } from "@/features/videos"
import { formatBytes, formatDuration } from "@/features/videos/format"
import { authStore } from "@/stores/auth"

export const Route = createFileRoute("/dashboard/")({
	component: DashboardHome,
})

function DashboardHome() {
	const { t } = useTranslation()
	const user = useStore(authStore, (s) => s.user)
	const { data: stats, isLoading } = useStatistics()

	return (
		<div className="p-8">
			<h1 className="text-3xl font-heading font-bold mb-2">
				{t("nav.dashboard")}
			</h1>
			<p className="text-muted-foreground mb-8">
				Welcome{user ? `, ${user.displayName}` : ""}
				{user ? ` (${user.role})` : ""}
			</p>

			<LiveStreamsCard />

			{isLoading && <div className="text-muted-foreground">Loading…</div>}

			{stats && (
				<>
					<div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-8">
						<StatCard
							label={t("stats.total_videos")}
							value={stats.total.toLocaleString()}
						/>
						<StatCard
							label={t("stats.total_size")}
							value={formatBytes(stats.total_size)}
						/>
						<StatCard
							label={t("stats.total_duration")}
							value={formatDuration(stats.total_duration_seconds)}
						/>
					</div>

					{stats.by_status.length > 0 && (
						<div>
							<h2 className="text-sm uppercase tracking-wide text-muted-foreground mb-2">
								{t("stats.by_status")}
							</h2>
							<div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
								{stats.by_status.map((bucket) => (
									<div
										key={bucket.status}
										className="rounded-md border border-border bg-card p-3"
									>
										<div className="text-xs text-muted-foreground">
											{t(`videos.status.${bucket.status}` as const, bucket.status)}
										</div>
										<div className="text-xl font-semibold mt-0.5">
											{bucket.count}
										</div>
									</div>
								))}
							</div>
						</div>
					)}
				</>
			)}
		</div>
	)
}

function StatCard({ label, value }: { label: string; value: string }) {
	return (
		<div className="rounded-lg border border-border bg-card p-4">
			<div className="text-xs uppercase tracking-wide text-muted-foreground">
				{label}
			</div>
			<div className="text-2xl font-semibold mt-1">{value}</div>
		</div>
	)
}
