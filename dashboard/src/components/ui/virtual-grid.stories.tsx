import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import type { VideoResponse } from "@/api/generated/trpc";
import i18n from "@/i18n";
import { makeVideos } from "@/test/fixtures";
import { Skeleton } from "./skeleton";
import { VirtualGrid, VirtualGridSkeleton } from "./virtual-grid";

const VIDEOS = makeVideos(1000);

function PlaceholderTile({ video }: { video: VideoResponse }) {
	return (
		<div className="flex aspect-video flex-col justify-end gap-0.5 rounded-lg border border-border bg-card p-3">
			<div className="truncate text-sm font-medium">{video.title}</div>
			<div className="truncate text-xs text-muted-foreground">
				#{video.id} · {video.broadcaster_name}
			</div>
		</div>
	);
}

const meta = preview.meta({
	title: "UI/VirtualGrid",
	component: VirtualGrid<VideoResponse>,
	args: {
		items: VIDEOS,
		getItemKey: (video) => video.id,
		renderItem: (video) => <PlaceholderTile video={video} />,
		minItemWidth: 280,
		estimateRowHeight: 160,
		gap: 16,
	},
	argTypes: {
		items: { control: false },
	},
});

export const Default = meta.story();

export const NarrowItems = meta.story({
	args: {
		minItemWidth: 140,
		estimateRowHeight: 80,
		gap: 8,
		overscan: 6,
	},
});

export const FewItems = meta.story({
	args: { items: VIDEOS.slice(0, 5) },
});

export const Loading = meta.story({
	render: ({ minItemWidth, gap }) => (
		<VirtualGridSkeleton
			count={12}
			minItemWidth={minItemWidth}
			gap={gap}
			renderItem={(index) => (
				<Skeleton
					data-slot-index={index}
					className="aspect-video w-full rounded-lg"
				/>
			)}
		/>
	),
	play: async ({ canvas }) => {
		const status = canvas.getByRole("status", {
			name: i18n.t("common.loading"),
		});
		await expect(status).toBeVisible();
		const slots = [...status.querySelectorAll("[data-slot-index]")].map(
			(slot) => Number(slot.getAttribute("data-slot-index")),
		);
		await expect(slots).toEqual(Array.from({ length: 12 }, (_, i) => i));
	},
});
