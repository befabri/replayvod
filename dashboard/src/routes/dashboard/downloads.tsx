import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { RunningDownloads } from "@/features/dashboard";

export const Route = createFileRoute("/dashboard/downloads")({
	component: DownloadsPage,
});

// The Downloads page is the full live view of in-flight recordings: the same
// SSE-fed card the dashboard home shows as a top-N peek, here uncapped and with
// a cancel control. Live recordings have no queued state (the downloader
// rejects at capacity rather than queuing), so "running now" is the whole
// story for them; VOD archives do queue, and their queue lives on the Archive
// page while a running archive shows here like any other job.
function DownloadsPage() {
	const { t } = useTranslation();
	return (
		<TitledLayout title={t("downloads.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("downloads.description")}
			</p>
			<RunningDownloads />
		</TitledLayout>
	);
}
