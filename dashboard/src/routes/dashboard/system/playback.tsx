import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import {
	PlaybackCacheCard,
	PlaybackCacheCardSkeleton,
} from "@/features/system/components/PlaybackCacheCard";
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
				<div className="grid gap-6">
					<PlaybackCacheCardSkeleton />
				</div>
			)}
			{config.isError && (
				<Alert variant="destructive">
					{config.error?.message ?? t("playback_cache.load_failed")}
				</Alert>
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
