import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { RunningDownloads } from "@/features/dashboard";

export const Route = createFileRoute("/dashboard/downloads")({
	component: DownloadsPage,
});

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
