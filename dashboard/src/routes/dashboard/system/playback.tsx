import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { PlaybackCacheCard } from "@/features/system/components/PlaybackCacheCard";
import { usePlaybackCacheConfig } from "@/features/system/queries";
import { requireRole } from "@/lib/route-guards";

export const Route = createFileRoute("/dashboard/system/playback")({
	beforeLoad: requireRole("owner"),
	component: PlaybackCachePage,
});

function PlaybackCachePage() {
	const { t } = useTranslation();
	const config = usePlaybackCacheConfig();

	return (
		<TitledLayout title={t("playback_cache.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("playback_cache.page_description")}
			</p>

			{config.isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{config.isError && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{config.error?.message ?? t("playback_cache.load_failed")}
				</div>
			)}
			{config.data && (
				<div className="grid gap-6">
					<PlaybackCacheCard
						key={`${config.data.enabled}-${config.data.max_percent}-${config.data.auto_generate}`}
						data={config.data}
					/>
				</div>
			)}
		</TitledLayout>
	);
}
