import preview from "#.storybook/preview";
import { makeVideos, VIDEO_QUALITIES } from "@/test/fixtures";
import { QualityTag } from "./quality-tag";

const meta = preview.meta({
	title: "UI/QualityTag",
	component: QualityTag,
	args: { children: VIDEO_QUALITIES[0] },
});

export const Default = meta.story();

export const Qualities = meta.story({
	render: (args) => (
		<div className="flex flex-wrap items-center gap-2">
			{VIDEO_QUALITIES.map((quality) => (
				<QualityTag key={quality} {...args}>
					{quality}
				</QualityTag>
			))}
		</div>
	),
});

export const InList = meta.story({
	render: (args) => (
		<ul className="w-96 divide-y divide-border rounded-lg bg-card p-3 text-sm">
			{makeVideos(3).map((video) => (
				<li
					key={video.id}
					className="flex items-center gap-2 py-2 first:pt-0 last:pb-0"
				>
					<span className="min-w-0 flex-1 truncate font-medium">
						{video.broadcaster_name}
					</span>
					<QualityTag {...args}>{video.quality}</QualityTag>
				</li>
			))}
		</ul>
	),
});
