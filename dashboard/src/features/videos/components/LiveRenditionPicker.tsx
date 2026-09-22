import { useTranslation } from "react-i18next";
import {
	OwnerSessionNotice,
	useIsOwner,
	ViewerSessionNotice,
} from "@/components/quality-session-hint";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import {
	type RenditionOption,
	renditionLabel,
} from "@/features/videos/renditions";

export function LiveRenditionPicker({
	id,
	options,
	loading,
	anonymous,
	value,
	onChange,
	disabled,
}: {
	id: string;
	options: RenditionOption[];
	loading: boolean;
	anonymous: boolean;
	value: number | null;
	onChange: (height: number) => void;
	disabled: boolean;
}) {
	const { t } = useTranslation();
	const isOwner = useIsOwner();
	const items = options.map((o) => ({
		value: String(o.height),
		label: renditionLabel(o),
	}));
	return (
		<>
			<Select
				value={value === null ? null : String(value)}
				onValueChange={(v) => {
					if (typeof v === "string") onChange(Number(v));
				}}
				disabled={disabled || loading}
				items={items}
			>
				<SelectTrigger id={id} data-testid="live-rendition-picker">
					<SelectValue
						placeholder={t(
							loading
								? "videos.download.renditions_loading"
								: "videos.download.renditions_select",
						)}
					/>
				</SelectTrigger>
				<SelectContent>
					{items.map((item) => (
						<SelectItem key={item.value} value={item.value}>
							{item.label}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			{!loading && (
				<p className="text-xs text-muted-foreground">
					{t(
						value === null
							? "videos.download.renditions_no_match"
							: "videos.download.renditions_hint",
					)}
				</p>
			)}
			{!loading &&
				anonymous &&
				(isOwner ? (
					<OwnerSessionNotice
						testId="renditions-session-notice"
						text={t("videos.download.renditions_anonymous_owner")}
					/>
				) : (
					<ViewerSessionNotice
						testId="renditions-session-notice"
						text={t("videos.download.renditions_anonymous_viewer")}
					/>
				))}
		</>
	);
}
