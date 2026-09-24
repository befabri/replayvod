import {
	RowsIcon,
	SortAscendingIcon,
	SquaresFourIcon,
} from "@phosphor-icons/react";
import { type ReactNode, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { FilterTabs } from "@/components/ui/filter-tabs";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
	ANY_FILTER,
	isOneOf,
	VIDEO_DURATION_FILTERS,
	VIDEO_LIST_SORT_KEYS,
	VIDEO_LIST_STATUSES,
	VIDEO_LIST_TABS,
	VIDEO_LIST_VIEWS,
	VIDEO_QUALITY_LADDER,
	VIDEO_SOURCE_FILTERS,
	type VideoListFilters,
	type VideoListSortKey,
	type VideoListTab,
	type VideoListView,
} from "@/features/videos/list-search";

type Option = { value: string; label: string };

export function VideoSortSelect({
	current,
	onChange,
}: {
	current: VideoListSortKey;
	onChange: (key: VideoListSortKey) => void;
}) {
	const { t } = useTranslation();
	const label = t(`videos.sort.${current}`);
	return (
		<Select
			value={current}
			onValueChange={(next) => onChange(next as VideoListSortKey)}
		>
			<SelectTrigger
				variant="chip"
				className="min-w-[150px]"
				aria-label={t("videos.sort_label")}
			>
				<div className="flex items-center gap-2">
					<SortAscendingIcon className="size-4 text-muted-foreground" />
					<span className="truncate text-sm font-medium">{label}</span>
				</div>
			</SelectTrigger>
			<SelectContent>
				{VIDEO_LIST_SORT_KEYS.map((key) => (
					<SelectItem key={key} value={key}>
						{t(`videos.sort.${key}`)}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}

const VIEW_ICONS: Record<VideoListView, ReactNode> = {
	grid: <SquaresFourIcon className="size-4" />,
	table: <RowsIcon className="size-4" />,
};

export function VideoViewToggle({
	current,
	onChange,
}: {
	current: VideoListView;
	onChange: (mode: VideoListView) => void;
}) {
	const { t } = useTranslation();
	return (
		<ToggleGroup
			aria-label={t("videos.view_label")}
			value={[current]}
			onValueChange={(next) => {
				const mode = next[0];
				if (isOneOf(VIDEO_LIST_VIEWS, mode)) onChange(mode);
			}}
		>
			{VIDEO_LIST_VIEWS.map((mode) => (
				<ToggleGroupItem key={mode} value={mode}>
					{VIEW_ICONS[mode]}
					{t(`videos.view.${mode}`)}
				</ToggleGroupItem>
			))}
		</ToggleGroup>
	);
}

export function VideoScopeTabs({
	current,
	counts,
	onChange,
}: {
	current: VideoListTab;
	counts: Partial<Record<VideoListTab, number>>;
	onChange: (key: VideoListTab) => void;
}) {
	const { t } = useTranslation();
	return (
		<FilterTabs
			value={current}
			onChange={(value) => onChange(value as VideoListTab)}
			options={VIDEO_LIST_TABS.map((key) => ({
				value: key,
				label: t(`videos.tabs.${key}`),
				count: counts[key],
			}))}
		/>
	);
}

export function FilterChipSelect({
	label,
	value,
	options,
	onChange,
}: {
	label: string;
	value: string;
	options: Option[];
	onChange: (value: string) => void;
}) {
	const selected =
		options.find((option) => option.value === value) ?? options[0];
	return (
		<Select value={value} onValueChange={(next) => onChange(String(next))}>
			<SelectTrigger
				variant="chip"
				className="min-w-[138px]"
				aria-label={label}
			>
				<span className="truncate text-sm">
					<span className="text-muted-foreground">{label}:</span>{" "}
					<span className="font-medium text-foreground">{selected?.label}</span>
				</span>
			</SelectTrigger>
			<SelectContent>
				{options.map((option) => (
					<SelectItem key={option.value} value={option.value}>
						{option.label}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}

function withSelectedOption(options: Option[], selected: string | undefined) {
	if (!selected || options.some((o) => o.value === selected)) return options;
	return [...options, { value: selected, label: selected }];
}

export function VideoListFilterChips({
	filters,
	languages,
	onChange,
}: {
	filters: VideoListFilters;
	languages: ReadonlySet<string>;
	onChange: (patch: VideoListFilters) => void;
}) {
	const { t } = useTranslation();
	const { status, quality, language, duration, source } = filters;
	const statusOptions = useMemo(
		() =>
			withSelectedOption(
				[
					{ value: ANY_FILTER, label: t("videos.status_filter.any") },
					...VIDEO_LIST_STATUSES.map((key) => ({
						value: key,
						label: t(`videos.status_filter.${key}` as const),
					})),
				],
				status,
			),
		[status, t],
	);
	const qualityOptions = useMemo(
		() =>
			withSelectedOption(
				[
					{ value: ANY_FILTER, label: t("videos.filter_any") },
					...VIDEO_QUALITY_LADDER.map((q) => ({ value: q, label: q })),
				],
				quality,
			),
		[quality, t],
	);
	const languageOptions = useMemo(
		() =>
			withSelectedOption(
				[
					{ value: ANY_FILTER, label: t("videos.filter_any") },
					...[...languages]
						.sort((a, b) => a.localeCompare(b))
						.map((value) => ({ value, label: value.toUpperCase() })),
				],
				language,
			),
		[languages, language, t],
	);
	const durationOptions = useMemo(
		() => [
			{ value: ANY_FILTER, label: t("videos.duration_any") },
			...VIDEO_DURATION_FILTERS.map((key) => ({
				value: key,
				label: t(`videos.duration_${key}` as const),
			})),
		],
		[t],
	);
	const sourceOptions = useMemo(
		() => [
			{ value: ANY_FILTER, label: t("videos.source_filter.any") },
			...VIDEO_SOURCE_FILTERS.map((key) => ({
				value: key,
				label: t(`videos.source_filter.${key}` as const),
			})),
		],
		[t],
	);

	return (
		<div className="flex flex-wrap items-center gap-2.5">
			<FilterChipSelect
				label={t("videos.filter_status")}
				value={status ?? ANY_FILTER}
				options={statusOptions}
				onChange={(value) =>
					onChange({
						status: isOneOf(VIDEO_LIST_STATUSES, value) ? value : undefined,
					})
				}
			/>
			<FilterChipSelect
				label={t("videos.filter_quality")}
				value={quality ?? ANY_FILTER}
				options={qualityOptions}
				onChange={(value) =>
					onChange({ quality: value === ANY_FILTER ? undefined : value })
				}
			/>
			<FilterChipSelect
				label={t("videos.filter_language")}
				value={language ?? ANY_FILTER}
				options={languageOptions}
				onChange={(value) =>
					onChange({ language: value === ANY_FILTER ? undefined : value })
				}
			/>
			<FilterChipSelect
				label={t("videos.filter_duration")}
				value={duration ?? ANY_FILTER}
				options={durationOptions}
				onChange={(value) =>
					onChange({
						duration: isOneOf(VIDEO_DURATION_FILTERS, value)
							? value
							: undefined,
					})
				}
			/>
			<FilterChipSelect
				label={t("videos.filter_source")}
				value={source ?? ANY_FILTER}
				options={sourceOptions}
				onChange={(value) =>
					onChange({
						source: isOneOf(VIDEO_SOURCE_FILTERS, value) ? value : undefined,
					})
				}
			/>
		</div>
	);
}
