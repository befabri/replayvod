import { useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import type { EnqueueArchiveItem } from "@/api/generated/trpc";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { parseVodLines } from "@/features/archive/parse";
import { useEnqueueArchive } from "@/features/archive/queries";
import {
	type ArchiveSettings,
	archivePayloadSettings,
} from "@/features/archive/settings";
import { cn } from "@/lib/utils";
import { MAX_VODS_PER_ENQUEUE } from "../limits";
import { EnqueueResults } from "./EnqueueResults";

// ArchiveLinksForm is the paste box: one VOD link or id per line, nothing else
// required. Lines that are not VOD links are flagged before submit; the server
// answers per line (queued, already held, gone from Twitch) after.
export function ArchiveLinksForm({ settings }: { settings: ArchiveSettings }) {
	const { t } = useTranslation();
	const id = useId();
	const [text, setText] = useState("");
	const [results, setResults] = useState<EnqueueArchiveItem[]>([]);
	const enqueue = useEnqueueArchive();

	const lines = useMemo(() => parseVodLines(text), [text]);
	const valid = lines.filter((line) => line.vodId != null);
	const invalidCount = lines.length - valid.length;
	const tooMany = valid.length > MAX_VODS_PER_ENQUEUE;
	const canSubmit = valid.length > 0 && !tooMany && !enqueue.isPending;

	const submit = async () => {
		if (!canSubmit) return;
		try {
			const response = await enqueue.mutateAsync({
				vods: valid.map((line) => line.input),
				...archivePayloadSettings(settings),
			});
			const invalidLines: EnqueueArchiveItem[] = lines
				.filter((line) => line.vodId == null)
				.map((line) => ({
					input: line.input,
					status: "invalid",
					message: t("archive.result_invalid_hint"),
				}));
			setResults([...response.items, ...invalidLines]);
			const queued = response.items.filter((i) => i.status === "queued").length;
			if (queued > 0) {
				toast.success(t("archive.queued_toast", { count: queued }));
			} else {
				toast.info(t("archive.nothing_queued_toast"));
			}
			setText("");
		} catch (err) {
			toast.error(
				err instanceof Error && err.message
					? err.message
					: t("archive.enqueue_failed"),
			);
		}
	};

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				void submit();
			}}
			className="space-y-4"
		>
			<div className="space-y-2">
				<Label htmlFor={`${id}-links`}>{t("archive.links_label")}</Label>
				<textarea
					id={`${id}-links`}
					value={text}
					onChange={(e) => setText(e.target.value)}
					placeholder={t("archive.links_placeholder")}
					rows={5}
					spellCheck={false}
					className={cn(
						"flex w-full min-w-0 rounded-md border border-border bg-background px-3 py-2 font-mono text-sm shadow-xs transition-colors outline-none",
						"placeholder:font-sans placeholder:text-muted-foreground",
						"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
					)}
				/>
				<p className="text-xs text-muted-foreground">
					{tooMany
						? t("archive.links_too_many", { max: MAX_VODS_PER_ENQUEUE })
						: invalidCount > 0
							? t("archive.links_invalid", { count: invalidCount })
							: t("archive.links_hint")}
				</p>
			</div>

			<div className="flex items-center justify-end gap-3">
				<Button type="submit" disabled={!canSubmit}>
					{enqueue.isPending
						? t("common.saving")
						: valid.length > 0
							? t("archive.submit_count", { count: valid.length })
							: t("archive.submit")}
				</Button>
			</div>

			<EnqueueResults items={results} />
		</form>
	);
}
