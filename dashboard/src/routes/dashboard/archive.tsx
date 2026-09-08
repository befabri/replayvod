import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useArchiveSettings } from "@/features/archive";
import { ArchiveLinksForm } from "@/features/archive/components/ArchiveLinksForm";
import { ArchiveQueue } from "@/features/archive/components/ArchiveQueue";
import { ArchiveSettingsFields } from "@/features/archive/components/ArchiveSettingsFields";
import { ChannelVodBrowser } from "@/features/archive/components/ChannelVodBrowser";
import { useCanManageVideos } from "@/features/videos/permissions";

export const Route = createFileRoute("/dashboard/archive")({
	component: ArchivePage,
});

// The Archive page is where past broadcasts enter the library: paste VOD links
// or browse a channel, pick a recording quality once, and the queue below shows
// what is waiting and downloading. Viewers only see the queue; queueing is
// admin-level on the server like every other download control.
function ArchivePage() {
	const { t } = useTranslation();
	const canManage = useCanManageVideos();
	const settings = useArchiveSettings();

	return (
		<TitledLayout
			title={t("archive.title")}
			description={t("archive.description")}
		>
			<div className="space-y-10">
				{canManage ? (
					<section className="rounded-2xl border border-border bg-card/70 px-4 py-4 sm:px-6 sm:py-5">
						<Tabs defaultValue="links">
							<TabsList>
								<TabsTrigger value="links">
									{t("archive.tab_links")}
								</TabsTrigger>
								<TabsTrigger value="channel">
									{t("archive.tab_channel")}
								</TabsTrigger>
							</TabsList>
							<TabsContent value="links" className="pt-4">
								<ArchiveLinksForm settings={settings.settings} />
							</TabsContent>
							<TabsContent value="channel" className="pt-4">
								<ChannelVodBrowser settings={settings.settings} />
							</TabsContent>
						</Tabs>
						<details className="mt-6 border-t border-border pt-4">
							<summary className="cursor-pointer text-sm font-medium">
								{t("archive.settings_title")}
							</summary>
							<div className="pt-4">
								<ArchiveSettingsFields controller={settings} />
							</div>
						</details>
					</section>
				) : null}
				<ArchiveQueue />
			</div>
		</TitledLayout>
	);
}
