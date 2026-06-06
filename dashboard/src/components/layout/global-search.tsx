import {
	CircleNotchIcon,
	MagnifyingGlassIcon,
	XIcon,
} from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
	CategoryResponse,
	ChannelResponse,
	VideoResponse,
} from "@/api/generated/trpc";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Combobox,
	ComboboxCollection,
	ComboboxContent,
	ComboboxGroup,
	ComboboxGroupLabel,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
} from "@/components/ui/combobox";
import {
	Dialog,
	DialogContent,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { API_URL } from "@/env";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { useCategorySearchWithVideos } from "@/features/categories/queries";
import { useChannelSearch } from "@/features/channels/queries";
import { channelLabel, useVideoSearch } from "@/features/videos";
import { VideoStatusBadge } from "@/features/videos/components/VideoStatusBadge";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { cn } from "@/lib/utils";

type SearchScope = "all" | "videos" | "channels" | "categories";

type SearchResult =
	| { id: string; kind: "video"; item: VideoResponse }
	| { id: string; kind: "channel"; item: ChannelResponse }
	| { id: string; kind: "category"; item: CategoryResponse };

type SearchGroup = {
	kind: Exclude<SearchScope, "all">;
	label: string;
	items: SearchResult[];
};

const SEARCH_SCOPES: SearchScope[] = [
	"all",
	"videos",
	"channels",
	"categories",
];

export function GlobalSearch({
	className,
	autoFocus,
	defaultScope = "all",
	onNavigate,
	shortcut,
}: {
	className?: string;
	autoFocus?: boolean;
	defaultScope?: SearchScope;
	onNavigate?: () => void;
	shortcut?: boolean;
}) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const inputRef = useRef<HTMLInputElement>(null);
	const barRef = useRef<HTMLFormElement>(null);
	const [query, setQuery] = useState("");
	const [scope, setScope] = useState<SearchScope>(defaultScope);
	const [open, setOpen] = useState(false);
	const shortcutLabel = useShortcutLabel(shortcut);
	const inputQuery = query.trim();
	const debouncedQuery = useDebouncedValue(query, 180).trim();
	const queryReady = inputQuery.length > 0;
	const queryFresh = queryReady && inputQuery === debouncedQuery;
	const debouncedReady = debouncedQuery.length > 0;
	const videoScopeActive = scope === "all" || scope === "videos";
	const channelScopeActive = scope === "all" || scope === "channels";
	const categoryScopeActive = scope === "all" || scope === "categories";
	const videoLimit = scope === "all" ? 4 : 8;
	const otherLimit = scope === "all" ? 3 : 8;

	const videos = useVideoSearch(debouncedQuery, videoLimit, {
		enabled: open && debouncedReady && videoScopeActive,
	});
	const channels = useChannelSearch(debouncedQuery, otherLimit, {
		enabled: open && debouncedReady && channelScopeActive,
	});
	const categories = useCategorySearchWithVideos(debouncedQuery, otherLimit, {
		enabled: open && debouncedReady && categoryScopeActive,
	});

	const videoResults =
		queryFresh && videoScopeActive ? (videos.data ?? []) : [];
	const channelResults =
		queryFresh && channelScopeActive ? (channels.data ?? []) : [];
	const categoryResults =
		queryFresh && categoryScopeActive ? (categories.data ?? []) : [];

	const groups = useMemo<SearchGroup[]>(() => {
		const out: SearchGroup[] = [];
		if (videoScopeActive && videoResults.length > 0) {
			out.push({
				kind: "videos",
				label: t("search.group.videos"),
				items: videoResults.map((video) => ({
					id: `video-${video.id}`,
					kind: "video",
					item: video,
				})),
			});
		}
		if (channelScopeActive && channelResults.length > 0) {
			out.push({
				kind: "channels",
				label: t("search.group.channels"),
				items: channelResults.map((channel) => ({
					id: `channel-${channel.broadcaster_id}`,
					kind: "channel",
					item: channel,
				})),
			});
		}
		if (categoryScopeActive && categoryResults.length > 0) {
			out.push({
				kind: "categories",
				label: t("search.group.categories"),
				items: categoryResults.map((category) => ({
					id: `category-${category.id}`,
					kind: "category",
					item: category,
				})),
			});
		}
		return out;
	}, [
		t,
		videoScopeActive,
		channelScopeActive,
		categoryScopeActive,
		videoResults,
		channelResults,
		categoryResults,
	]);

	const searchItems = useMemo(
		() => groups.flatMap((group) => group.items),
		[groups],
	);
	const hasResults = searchItems.length > 0;
	const isFetchingActiveScope =
		(videoScopeActive && videos.isFetching) ||
		(channelScopeActive && channels.isFetching) ||
		(categoryScopeActive && categories.isFetching);
	const isSearching = queryReady && (!queryFresh || isFetchingActiveScope);
	const showPanel = open && queryReady;
	const firstTarget = searchItems[0] ?? null;

	const focusInput = useCallback(() => {
		inputRef.current?.focus();
		inputRef.current?.select();
	}, []);

	useEffect(() => {
		if (!shortcut) return;
		const onKeyDown = (event: KeyboardEvent) => {
			if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
				event.preventDefault();
				focusInput();
				// ⌘K focuses programmatically, bypassing openOnInputClick, so reopen
				// the panel ourselves when there's already a query to resurface.
				if (inputRef.current?.value.trim()) setOpen(true);
			}
		};
		window.addEventListener("keydown", onKeyDown);
		return () => window.removeEventListener("keydown", onKeyDown);
	}, [shortcut, focusInput]);

	const close = () => setOpen(false);
	const finishNavigation = () => {
		setQuery("");
		close();
		onNavigate?.();
	};
	const goToVideo = (video: VideoResponse) => {
		finishNavigation();
		void navigate({
			to: "/dashboard/watch/$videoId",
			params: { videoId: String(video.id) },
			search: { t: undefined },
		});
	};
	const goToChannel = (channel: ChannelResponse) => {
		finishNavigation();
		void navigate({
			to: "/dashboard/channels/$channelId",
			params: { channelId: channel.broadcaster_id },
		});
	};
	const goToCategory = (category: CategoryResponse) => {
		finishNavigation();
		void navigate({
			to: "/dashboard/categories/$categoryId",
			params: { categoryId: category.id },
		});
	};

	const selectResult = (result: SearchResult | null) => {
		if (!result) return;
		if (result.kind === "video") goToVideo(result.item);
		if (result.kind === "channel") goToChannel(result.item);
		if (result.kind === "category") goToCategory(result.item);
	};

	return (
		<Combobox<SearchResult>
			items={searchItems}
			filter={null}
			inputValue={query}
			open={showPanel}
			value={null}
			autoHighlight
			inputRef={inputRef}
			openOnInputClick={queryReady}
			onOpenChange={(nextOpen) => setOpen(nextOpen)}
			onInputValueChange={(value, details) => {
				// Selecting a result makes Base UI refill the input with that item's
				// label; ignore it so finishNavigation's clear wins on click.
				if (details.reason === "item-press") return;
				setQuery(value);
				setOpen(value.trim().length > 0);
			}}
			onValueChange={(result) => selectResult(result)}
			itemToStringLabel={searchResultLabel}
			itemToStringValue={(result) => result.id}
			isItemEqualToValue={(a, b) => a.id === b.id}
		>
			<div className={cn("relative min-w-0", className)}>
				<form
					ref={barRef}
					className="flex h-9 min-w-0 items-center rounded-md border border-border bg-background shadow-xs transition-[border-color,box-shadow] duration-200 hover:border-ring-muted focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50"
					onSubmit={(event) => {
						event.preventDefault();
						selectResult(firstTarget);
					}}
				>
					<Select
						value={scope}
						onValueChange={(value) => {
							setScope(isSearchScope(value) ? value : "all");
							setOpen(inputQuery.length > 0);
						}}
					>
						<SelectTrigger
							aria-label={t("search.scope_label")}
							className="h-7 w-[6.5rem] shrink-0 rounded-sm border-0 bg-transparent px-2 shadow-none focus-visible:ring-0 sm:w-[7.25rem]"
						>
							<SelectValue>
								{(value: unknown) =>
									t(`search.scope.${isSearchScope(value) ? value : "all"}`)
								}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{SEARCH_SCOPES.map((item) => (
								<SelectItem key={item} value={item}>
									{t(`search.scope.${item}`)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
					<span aria-hidden className="h-5 w-px bg-border" />
					<div className="relative min-w-0 flex-1">
						<span
							aria-hidden
							className="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
						>
							<MagnifyingGlassIcon
								className={cn(
									"absolute inset-0 size-4 transition-opacity duration-200",
									isSearching ? "opacity-0" : "opacity-100",
								)}
							/>
							<CircleNotchIcon
								className={cn(
									"absolute inset-0 size-4 animate-spin text-primary transition-opacity duration-200",
									isSearching ? "opacity-100" : "opacity-0",
								)}
							/>
						</span>
						<ComboboxInput
							autoFocus={autoFocus}
							aria-label={t("search.input_label")}
							placeholder={t("search.placeholder")}
							className="h-7 border-0 bg-transparent pl-8 pr-14 shadow-none focus-visible:border-0 focus-visible:ring-0"
						/>
						<div className="absolute right-1.5 top-1/2 flex -translate-y-1/2 items-center">
							{query ? (
								<Button
									type="button"
									variant="ghost"
									size="icon-xs"
									className="animate-in fade-in-0 zoom-in-95 duration-150"
									aria-label={t("search.clear")}
									onClick={() => {
										setQuery("");
										setOpen(false);
										focusInput();
									}}
								>
									<XIcon />
								</Button>
							) : shortcutLabel ? (
								<kbd className="pointer-events-none hidden select-none items-center rounded-sm border border-border bg-muted px-1.5 font-mono text-[10px] font-medium tracking-wide text-muted-foreground sm:inline-flex">
									{shortcutLabel}
								</kbd>
							) : null}
						</div>
					</div>
				</form>

				<ComboboxContent
					anchor={barRef}
					align="start"
					sideOffset={8}
					className="w-[var(--anchor-width)] max-h-[min(34rem,var(--available-height))] p-1.5"
				>
					{isSearching && !hasResults ? (
						<SearchLoading />
					) : (
						<>
							<ComboboxList<SearchResult>>
								{groups.map((group) => (
									<ComboboxGroup key={group.kind} items={group.items}>
										<ComboboxGroupLabel>
											<span>{group.label}</span>
											<Badge variant="muted" className="tabular-nums">
												{group.items.length}
											</Badge>
										</ComboboxGroupLabel>
										<div className="flex flex-col gap-1">
											<ComboboxCollection<SearchResult>>
												{(result) => (
													<SearchResultItem
														key={result.id}
														result={result}
														query={debouncedQuery}
														t={t}
													/>
												)}
											</ComboboxCollection>
										</div>
									</ComboboxGroup>
								))}
							</ComboboxList>
							{queryReady && !isSearching && !hasResults && (
								<div className="px-3 py-6 text-center text-sm text-muted-foreground">
									{t("search.no_results")}
								</div>
							)}
						</>
					)}
				</ComboboxContent>
			</div>
		</Combobox>
	);
}

export function GlobalSearchDialog({ className }: { className?: string }) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger
				render={(triggerProps) => (
					<Button
						type="button"
						variant="ghost"
						size="icon-sm"
						className={className}
						aria-label={t("search.open")}
						{...triggerProps}
					>
						<MagnifyingGlassIcon />
					</Button>
				)}
			/>
			{open && (
				<DialogContent className="top-4 max-w-[calc(100vw-1rem)] translate-y-0 p-3 sm:hidden">
					<DialogTitle className="sr-only">{t("search.title")}</DialogTitle>
					<GlobalSearch
						autoFocus
						onNavigate={() => setOpen(false)}
						className="w-full"
					/>
				</DialogContent>
			)}
		</Dialog>
	);
}

function SearchResultItem({
	result,
	query,
	t,
}: {
	result: SearchResult;
	query: string;
	t: TFunction;
}) {
	return (
		<ComboboxItem
			value={result}
			className="h-auto w-full cursor-pointer justify-start gap-3 px-2 py-2 text-left whitespace-normal data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
		>
			{result.kind === "video" && (
				<VideoResultRow video={result.item} query={query} t={t} />
			)}
			{result.kind === "channel" && (
				<ChannelResultRow channel={result.item} query={query} />
			)}
			{result.kind === "category" && (
				<CategoryResultRow category={result.item} query={query} />
			)}
		</ComboboxItem>
	);
}

function VideoResultRow({
	video,
	query,
	t,
}: {
	video: VideoResponse;
	query: string;
	t: TFunction;
}) {
	const title = video.title?.trim() || video.display_name;
	const channel = channelLabel(video);
	const thumb = video.thumbnail
		? `${API_URL}/api/v1/thumbnails/${video.thumbnail.replace(/^thumbnails\//, "")}`
		: null;
	return (
		<>
			<div className="flex aspect-video w-20 shrink-0 items-center justify-center overflow-hidden rounded-sm bg-muted text-xs text-muted-foreground">
				{thumb ? (
					<img src={thumb} alt="" className="h-full w-full object-cover" />
				) : (
					<span>VOD</span>
				)}
			</div>
			<div className="min-w-0 flex-1">
				<div className="line-clamp-1 font-medium text-foreground">
					<HighlightedText text={title} query={query} />
				</div>
				<div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
					<span className="truncate">
						<HighlightedText text={channel} query={query} />
					</span>
					{video.primary_category_name ? (
						<>
							<span aria-hidden className="opacity-50">
								/
							</span>
							<span className="truncate">
								<HighlightedText
									text={video.primary_category_name}
									query={query}
								/>
							</span>
						</>
					) : null}
				</div>
			</div>
			<VideoStatusBadge
				status={video.status}
				completionKind={video.completion_kind}
				t={t}
			/>
		</>
	);
}

function ChannelResultRow({
	channel,
	query,
}: {
	channel: ChannelResponse;
	query: string;
}) {
	return (
		<>
			<Avatar
				src={channel.profile_image_url}
				name={channel.broadcaster_name}
				alt={channel.broadcaster_name}
				size="md"
			/>
			<div className="min-w-0 flex-1">
				<div className="truncate font-medium text-foreground">
					<HighlightedText text={channel.broadcaster_name} query={query} />
				</div>
				<div className="truncate font-mono text-xs text-muted-foreground">
					@<HighlightedText text={channel.broadcaster_login} query={query} />
				</div>
			</div>
		</>
	);
}

function CategoryResultRow({
	category,
	query,
}: {
	category: CategoryResponse;
	query: string;
}) {
	return (
		<>
			<CategoryThumb category={category} />
			<div className="min-w-0 flex-1">
				<div className="truncate font-medium text-foreground">
					<HighlightedText text={category.name} query={query} />
				</div>
				<div className="truncate font-mono text-xs text-muted-foreground">
					<HighlightedText text={category.id} query={query} />
				</div>
			</div>
		</>
	);
}

function CategoryThumb({ category }: { category: CategoryResponse }) {
	return (
		<CategoryBoxArt
			url={category.box_art_url}
			name={category.name}
			width={36}
			height={48}
			sizes="36px"
			decorative
			placeholderIconSize={16}
			className="w-9 rounded-sm shrink-0"
		/>
	);
}

function SearchLoading() {
	return (
		<div className="space-y-2 p-2">
			<Skeleton className="h-4 w-24" />
			<Skeleton className="h-14 w-full" />
			<Skeleton className="h-14 w-full" />
			<Skeleton className="h-14 w-full" />
		</div>
	);
}

function HighlightedText({ text, query }: { text: string; query: string }) {
	const needle = query.trim().toLowerCase();
	if (!needle) return <>{text}</>;
	const index = text.toLowerCase().indexOf(needle);
	if (index === -1) return <>{text}</>;
	return (
		<>
			{text.slice(0, index)}
			<mark className="rounded-[0.2rem] bg-primary/25 px-0.5 text-foreground">
				{text.slice(index, index + needle.length)}
			</mark>
			{text.slice(index + needle.length)}
		</>
	);
}

// ⌘K on Apple platforms, Ctrl K elsewhere. The app is prerendered (TanStack
// Start), where `navigator` doesn't exist, so the platform is read after mount
// via state rather than during render: reading it while rendering would crash
// the prerender or, on the client, hydrate a different label than the server
// emitted. Until resolved (and whenever the hint is disabled) it stays null and
// nothing renders.
function useShortcutLabel(enabled: boolean | undefined): string | null {
	const [label, setLabel] = useState<string | null>(null);
	useEffect(() => {
		if (!enabled) return;
		const apple = /mac|iphone|ipad|ipod/i.test(
			navigator.platform || navigator.userAgent,
		);
		setLabel(apple ? "⌘K" : "Ctrl K");
	}, [enabled]);
	return label;
}

function searchResultLabel(result: SearchResult): string {
	if (result.kind === "video") {
		return result.item.title?.trim() || result.item.display_name;
	}
	if (result.kind === "channel") {
		return result.item.broadcaster_name;
	}
	return result.item.name;
}

function isSearchScope(value: unknown): value is SearchScope {
	return (
		typeof value === "string" && SEARCH_SCOPES.includes(value as SearchScope)
	);
}
