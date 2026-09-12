import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FiltersFieldset } from "@/features/schedules/components/FiltersFieldset";
import { RecordingSettingsField } from "@/features/schedules/components/RecordingSettingsField";
import {
	buildSchedulePayload,
	useScheduleForm,
} from "@/features/schedules/form";
import { useCreateSchedule } from "@/features/schedules/queries";
import type { ScheduleFormValues } from "@/features/schedules/schema";
import { DirectDownloadForm } from "./DirectDownloadForm";

type DownloadTab = "now" | "schedule";

// ChannelDownloadDialog is the channel detail page's download entry
// point. It folds two distinct actions behind one button: an instant
// "download now" (records the current live stream, so it only works while
// the channel is live) and "schedule" (an auto-download rule that fires
// every time the channel goes live, with the same filters as the
// schedules page). The direct form checks the broadcaster when opened;
// followed-channel membership only supplies the avatar's live indicator.
export function ChannelDownloadDialog({
	broadcasterId,
	broadcasterName,
	broadcasterLogin,
	profileImageUrl,
	isLive,
	children,
}: {
	broadcasterId: string;
	broadcasterName: string;
	broadcasterLogin?: string;
	profileImageUrl?: string;
	isLive: boolean;
	children: React.ReactNode;
}) {
	const [open, setOpen] = useState(false);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger render={children as React.ReactElement} />
			{open && (
				<ChannelDownloadDialogBody
					broadcasterId={broadcasterId}
					broadcasterName={broadcasterName}
					broadcasterLogin={broadcasterLogin}
					profileImageUrl={profileImageUrl}
					isLive={isLive}
					onClose={() => setOpen(false)}
				/>
			)}
		</Dialog>
	);
}

function ChannelDownloadDialogBody({
	broadcasterId,
	broadcasterName,
	broadcasterLogin,
	profileImageUrl,
	isLive,
	onClose,
}: {
	broadcasterId: string;
	broadcasterName: string;
	broadcasterLogin?: string;
	profileImageUrl?: string;
	isLive: boolean;
	onClose: () => void;
}) {
	const { t } = useTranslation();
	// Absence from the followed-stream snapshot does not mean offline. Start
	// with the direct check and offer scheduling once offline is confirmed.
	const [tab, setTab] = useState<DownloadTab>("now");

	return (
		<DialogContent className="max-w-xl max-h-[88vh] overflow-y-auto">
			<DialogHeader className="flex-row items-center gap-3 space-y-0 text-left">
				<Avatar
					src={profileImageUrl}
					name={broadcasterName}
					alt={broadcasterName}
					size="md"
					isLive={isLive}
					liveRingClass="ring-popover"
				/>
				<div className="flex min-w-0 flex-col">
					<DialogTitle className="truncate text-left">
						{broadcasterName}
					</DialogTitle>
					<DialogDescription className="text-left">
						{broadcasterLogin
							? `@${broadcasterLogin} · ${t("videos.download.description")}`
							: t("videos.download.description")}
					</DialogDescription>
				</div>
			</DialogHeader>

			<Tabs value={tab} onValueChange={(v) => setTab(v as DownloadTab)}>
				<TabsList className="grid w-full grid-cols-2">
					<TabsTrigger value="now" className="cursor-pointer">
						{t("videos.download.tab_now")}
					</TabsTrigger>
					<TabsTrigger value="schedule" className="cursor-pointer">
						{t("videos.download.tab_schedule")}
					</TabsTrigger>
				</TabsList>

				<TabsContent value="now" className="mt-4">
					<DirectDownloadForm
						broadcasterId={broadcasterId}
						onClose={onClose}
						onSwitchToSchedule={() => setTab("schedule")}
					/>
				</TabsContent>

				<TabsContent value="schedule" className="mt-4">
					<ScheduleTab broadcasterId={broadcasterId} onClose={onClose} />
				</TabsContent>
			</Tabs>
		</DialogContent>
	);
}

function ScheduleTab({
	broadcasterId,
	onClose,
}: {
	broadcasterId: string;
	onClose: () => void;
}) {
	const { t } = useTranslation();
	const create = useCreateSchedule();

	// broadcaster_id is fixed to this channel (no picker): the dialog is
	// scoped to one channel, so the picker the schedules page shows would
	// be redundant here.
	const defaultValues: ScheduleFormValues = {
		broadcaster_id: broadcasterId,
		recording_type: "video",
		quality: "HIGH",
		force_h264: false,
		has_min_viewers: false,
		min_viewers: undefined,
		has_categories: false,
		category_ids: [],
		has_tags: false,
		tag_ids: [],
		is_delete_rediff: false,
		time_before_delete: undefined,
	};

	const form = useScheduleForm(defaultValues, async (value) => {
		try {
			await create.mutateAsync({
				...buildSchedulePayload(value),
				broadcaster_id: value.broadcaster_id.trim(),
				is_disabled: false,
			});
			toast.success(t("videos.download.schedule_created"));
			onClose();
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("schedules.create_failed"),
			);
		}
	});

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				e.stopPropagation();
				void form.handleSubmit();
			}}
			className="space-y-4"
		>
			<p className="text-sm text-muted-foreground">
				{t("videos.download.schedule_intro")}
			</p>

			<RecordingSettingsField form={form} />

			<FiltersFieldset form={form} />

			{create.isError && (
				<div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
					{create.error?.message ?? t("schedules.create_failed")}
				</div>
			)}

			<DialogFooter>
				<Button
					type="button"
					variant="outline"
					onClick={onClose}
					disabled={create.isPending}
				>
					{t("common.cancel")}
				</Button>
				<form.Subscribe
					selector={(s) => [s.canSubmit, s.isSubmitting] as const}
				>
					{([canSubmit, isSubmitting]) => (
						<Button
							type="submit"
							disabled={!canSubmit || isSubmitting || create.isPending}
						>
							{isSubmitting || create.isPending
								? t("common.saving")
								: t("videos.download.schedule_submit")}
						</Button>
					)}
				</form.Subscribe>
			</DialogFooter>
		</form>
	);
}
