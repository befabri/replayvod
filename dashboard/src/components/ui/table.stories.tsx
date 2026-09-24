import preview from "#.storybook/preview";
import { formatBytes, formatDuration } from "@/features/videos/format";
import { makeVideos } from "@/test/fixtures";
import {
	Table,
	TableBody,
	TableCaption,
	TableCell,
	TableFooter,
	TableHead,
	TableHeader,
	TableRow,
} from "./table";

const VIDEOS = makeVideos(5);
const TOTAL_BYTES = VIDEOS.reduce(
	(sum, video) => sum + (video.size_bytes ?? 0),
	0,
);

const meta = preview.meta({
	title: "UI/Table",
	component: Table,
});

function RecordingRows({ selectedId }: { selectedId?: number }) {
	return VIDEOS.map((video) => (
		<TableRow
			key={video.id}
			data-state={video.id === selectedId ? "selected" : undefined}
		>
			<TableCell className="font-medium">{video.broadcaster_name}</TableCell>
			<TableCell className="max-w-xs truncate">{video.title}</TableCell>
			<TableCell>{video.quality}</TableCell>
			<TableCell className="font-mono tabular-nums">
				{formatDuration(video.duration_seconds)}
			</TableCell>
			<TableCell className="text-right tabular-nums">
				{formatBytes(video.size_bytes)}
			</TableCell>
		</TableRow>
	));
}

function RecordingHeader() {
	return (
		<TableHeader>
			<TableRow>
				<TableHead>Channel</TableHead>
				<TableHead>Title</TableHead>
				<TableHead>Quality</TableHead>
				<TableHead>Duration</TableHead>
				<TableHead className="text-right">Size</TableHead>
			</TableRow>
		</TableHeader>
	);
}

export const Default = meta.story({
	render: (args) => (
		<Table {...args}>
			<RecordingHeader />
			<TableBody>
				<RecordingRows />
			</TableBody>
		</Table>
	),
});

export const WithFooterAndCaption = meta.story({
	render: (args) => (
		<Table {...args}>
			<TableCaption>Sample recordings.</TableCaption>
			<RecordingHeader />
			<TableBody>
				<RecordingRows />
			</TableBody>
			<TableFooter>
				<TableRow>
					<TableCell colSpan={4}>{VIDEOS.length} recordings</TableCell>
					<TableCell className="text-right tabular-nums">
						{formatBytes(TOTAL_BYTES)}
					</TableCell>
				</TableRow>
			</TableFooter>
		</Table>
	),
});

export const SelectedRow = meta.story({
	render: (args) => (
		<Table {...args}>
			<RecordingHeader />
			<TableBody>
				<RecordingRows selectedId={VIDEOS[2].id} />
			</TableBody>
		</Table>
	),
});
