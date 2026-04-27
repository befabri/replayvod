import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import type { VideoResponse } from "@/features/videos";
import { useVideoCategories, useVideoTitles } from "@/features/videos/queries";

// VideoDetails is the Watch-page panel that lives below the player.
// Three sections stacked:
//   1. Categories the stream was set to (box art + name, linkable)
//   2. Title history (every distinct title captured, chronological)
//   3. Technical metadata grid (size, quality, language, timestamps,
//      ids — useful for operators debugging a recording)
//
// The combined timeline dialog on VideoCard (StreamHistoryButton) is
// still the right fit on list views; here we have room to surface all
// three inline so users don't have to click to see context for the
// video they're watching.
export function VideoDetails({ video }: { video: VideoResponse }) {
	const { data: categories } = useVideoCategories(video.id);
	const { data: titles } = useVideoTitles(video.id);

	const hasCategories = !!categories && categories.length > 0;
	const hasMultipleTitles = !!titles && titles.length > 1;

	return (
		<div className="flex flex-col gap-6">
			{hasCategories && <CategoriesSection categories={categories} />}
			{hasMultipleTitles && <TitlesSection titles={titles} />}
			<MetadataSection video={video} />
		</div>
	);
}

function CategoriesSection({
	categories,
}: {
	categories: NonNullable<ReturnType<typeof useVideoCategories>["data"]>;
}) {
	const { t } = useTranslation();
	return (
		<section className="flex flex-col gap-3">
			<h3 className="text-sm uppercase tracking-wide text-muted-foreground">
				{t("videos.categories_inline_label")}
			</h3>
			<div className="flex flex-wrap gap-3">
				{categories.map((c) => (
					<Link
						key={c.id}
						// biome-ignore lint/suspicious/noExplicitAny: param route typing
						to={"/dashboard/categories/$categoryId" as any}
						// biome-ignore lint/suspicious/noExplicitAny: param route typing
						params={{ categoryId: c.id } as any}
						className="flex items-center gap-3 rounded-md bg-card p-2 pr-4 shadow-sm hover:ring-2 hover:ring-primary transition-all duration-75"
					>
						<CategoryBoxArt
							url={c.box_art_url}
							name={c.name}
							width={72}
							height={96}
							className="w-16 rounded-sm shrink-0"
						/>
						<span className="text-sm font-medium">{c.name}</span>
					</Link>
				))}
			</div>
		</section>
	);
}

function TitlesSection({
	titles,
}: {
	titles: NonNullable<ReturnType<typeof useVideoTitles>["data"]>;
}) {
	const { t } = useTranslation();
	return (
		<section className="flex flex-col gap-3">
			<h3 className="text-sm uppercase tracking-wide text-muted-foreground">
				{t("videos.title_history.heading")}
			</h3>
			<ol className="flex flex-col gap-2">
				{titles.map((title, idx) => (
					<li
						key={title.id}
						className="flex items-start gap-3 rounded-md bg-card px-3 py-2 shadow-sm"
					>
						<span className="text-xs font-mono text-muted-foreground w-6 shrink-0 pt-0.5">
							{idx + 1}.
						</span>
						<span className="text-sm leading-snug">{title.name}</span>
					</li>
				))}
			</ol>
		</section>
	);
}

function MetadataSection({ video }: { video: VideoResponse }) {
	const { t } = useTranslation();

	// Operator debugging IDs (stream_id, job_id) are available on the
	// VideoResponse but not surfaced here — they're only useful when
	// correlating with server logs, which isn't a Watch-page use case.
	const rows: Array<{ label: string; value: React.ReactNode }> = [];
	rows.push({ label: t("videos.quality"), value: video.quality });
	if (video.language)
		rows.push({ label: t("videos.language"), value: video.language });
	rows.push({
		label: t("videos.started_at"),
		value: new Date(video.start_download_at).toLocaleString(),
	});
	if (video.downloaded_at)
		rows.push({
			label: t("videos.downloaded_at"),
			value: new Date(video.downloaded_at).toLocaleString(),
		});

	return (
		<section className="flex flex-col gap-3">
			<h3 className="text-sm uppercase tracking-wide text-muted-foreground">
				{t("videos.metadata_heading")}
			</h3>
			<dl className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2 rounded-md bg-card p-4 shadow-sm">
				{rows.map((row) => (
					<div
						key={row.label}
						className="flex items-baseline justify-between gap-3 border-b border-border/50 last:border-b-0 sm:[&:nth-last-child(2)]:border-b-0 pb-1.5"
					>
						<dt className="text-xs text-muted-foreground">{row.label}</dt>
						<dd className="text-sm truncate">{row.value}</dd>
					</div>
				))}
			</dl>
		</section>
	);
}
