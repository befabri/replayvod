import { ArrowRightIcon } from "@phosphor-icons/react";
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
import {
	buildDirectDownloadPayload,
	useDirectDownloadForm,
} from "@/features/videos/download-form";
import { useTriggerDownload } from "@/features/videos/queries";
import { DirectDownloadFields } from "./DirectDownloadFields";

type DownloadTab = "now" | "schedule";

// ChannelDownloadDialog is the channel detail page's download entry
// point. It folds two distinct actions behind one button: an instant
// "download now" (records the current live stream, so it only works while
// the channel is live) and "schedule" (an auto-download rule that fires
// every time the channel goes live, with the same filters as the
// schedules page). Tabs keep both one click away; the dialog opens on the
// tab that's actionable given the channel's current live state.
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
	// Open on the actionable tab: "now" only does anything while the
	// channel is live, so offline visitors land on "schedule" instead.
	const [tab, setTab] = useState<DownloadTab>(isLive ? "now" : "schedule");

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
					<DownloadNowTab
						broadcasterId={broadcasterId}
						isLive={isLive}
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

function DownloadNowTab({
	broadcasterId,
	isLive,
	onClose,
	onSwitchToSchedule,
}: {
	broadcasterId: string;
	isLive: boolean;
	onClose: () => void;
	onSwitchToSchedule: () => void;
}) {
	const { t } = useTranslation();
	const trigger = useTriggerDownload();

	const form = useDirectDownloadForm(async (value) => {
		await trigger.mutateAsync(buildDirectDownloadPayload(broadcasterId, value));
		toast.success(t("videos.triggered"));
		onClose();
	});

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				e.stopPropagation();
				void form.handleSubmit();
			}}
			className="space-y-5"
		>
			<LiveStatus isLive={isLive} onSwitchToSchedule={onSwitchToSchedule} />

			<DirectDownloadFields form={form} disabled={!isLive} />

			{trigger.isError && (
				<div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
					{trigger.error?.message ?? t("videos.trigger_failed")}
				</div>
			)}

			<DialogFooter>
				<Button
					type="button"
					variant="outline"
					onClick={onClose}
					disabled={trigger.isPending}
				>
					{t("common.cancel")}
				</Button>
				<form.Subscribe
					selector={(s) => [s.canSubmit, s.isSubmitting] as const}
				>
					{([canSubmit, isSubmitting]) => (
						<Button
							type="submit"
							disabled={
								!isLive || !canSubmit || isSubmitting || trigger.isPending
							}
						>
							{isSubmitting || trigger.isPending
								? t("common.saving")
								: t("videos.trigger_submit")}
						</Button>
					)}
				</form.Subscribe>
			</DialogFooter>
		</form>
	);
}

// LiveStatus shows whether direct download is possible right now. Live is
// the Twitch-red dot (matching the avatar live ring); offline disables
// the action upstream and nudges the user toward a schedule, which works
// regardless of live state.
function LiveStatus({
	isLive,
	onSwitchToSchedule,
}: {
	isLive: boolean;
	onSwitchToSchedule: () => void;
}) {
	const { t } = useTranslation();

	if (isLive) {
		return (
			<div className="flex items-center gap-2 rounded-md border border-border bg-muted/40 px-3 py-2 text-sm">
				<span
					aria-hidden="true"
					className="size-2 shrink-0 animate-pulse rounded-full bg-destructive"
				/>
				<span className="font-medium">{t("videos.download.live_now")}</span>
				<span className="text-muted-foreground">
					· {t("videos.download.live_now_hint")}
				</span>
			</div>
		);
	}

	return (
		<div className="space-y-2 rounded-md border border-border bg-muted/40 px-3 py-3 text-sm">
			<div className="flex items-center gap-2">
				<span
					aria-hidden="true"
					className="size-2 shrink-0 rounded-full bg-muted-foreground/40"
				/>
				<span className="font-medium">{t("videos.download.offline")}</span>
				<span className="text-muted-foreground">
					· {t("videos.download.offline_hint")}
				</span>
			</div>
			<div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 pl-4">
				<span className="text-xs text-muted-foreground">
					{t("videos.download.offline_schedule_prompt")}
				</span>
				<Button
					type="button"
					variant="ghost"
					size="sm"
					onClick={onSwitchToSchedule}
					className="h-auto px-2 py-1 text-primary hover:text-primary"
				>
					{t("videos.download.set_up_schedule")}
					<ArrowRightIcon weight="bold" className="size-3.5" />
				</Button>
			</div>
		</div>
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
