import { useTranslation } from "react-i18next";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type {
	HistoryOutcome,
	HistoryView,
} from "@/features/videos/components/activityColumns";
import {
	HISTORY_MEDIA_SCOPES,
	HISTORY_OUTCOMES,
	isHistoryMedia,
} from "@/features/videos/history";

export function HistoryViewControls({
	view,
	counts,
	onViewChange,
}: {
	view: HistoryView;
	counts: Record<HistoryOutcome, number | undefined>;
	onViewChange: (next: HistoryView) => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
			<FilterTabs
				value={view.outcome}
				onChange={(next) => {
					onViewChange({ ...view, outcome: next as HistoryOutcome });
				}}
				options={HISTORY_OUTCOMES.map((key) => ({
					value: key,
					label: t(`history.outcome_${key}`),
					count: counts[key],
				}))}
			/>
			<div className="flex items-center gap-2 pb-3">
				<span className="text-xs text-muted-foreground">
					{t("history.media_label")}
				</span>
				<ToggleGroup
					value={[view.media]}
					onValueChange={(next) => {
						const media = next[0];
						if (isHistoryMedia(media)) {
							onViewChange({ ...view, media });
						}
					}}
				>
					{HISTORY_MEDIA_SCOPES.map((scope) => (
						<ToggleGroupItem key={scope} value={scope}>
							{t(`history.scope_${scope}`)}
						</ToggleGroupItem>
					))}
				</ToggleGroup>
			</div>
		</div>
	);
}
