import { fn } from "storybook/test";
import preview from "#.storybook/preview";
import { formatDuration } from "@/features/videos/format";
import { CHANNELS, makeVideos } from "@/test/fixtures";
import { Avatar } from "./avatar";
import { Badge } from "./badge";
import { Button } from "./button";
import {
	Card,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardTitle,
} from "./card";

const VIDEOS = makeVideos(4);

const meta = preview.meta({
	title: "UI/Card",
	component: Card,
});

export const Default = meta.story({
	render: (args) => (
		<Card {...args} className="w-[28rem]">
			<CardHeader className="sm:flex-row sm:items-start sm:justify-between">
				<div className="space-y-1.5">
					<CardTitle>{VIDEOS[0].title}</CardTitle>
					<CardDescription>{VIDEOS[0].broadcaster_name}</CardDescription>
				</div>
				<Badge variant="muted">{VIDEOS[0].quality}</Badge>
			</CardHeader>
			<CardContent className="space-y-2 text-sm">
				<div className="flex justify-between gap-4">
					<span className="text-muted-foreground">Duration</span>
					<span className="tabular-nums">
						{formatDuration(VIDEOS[0].duration_seconds)}
					</span>
				</div>
				<div className="flex justify-between gap-4">
					<span className="text-muted-foreground">Category</span>
					<span>{VIDEOS[0].primary_category_name}</span>
				</div>
			</CardContent>
			<CardFooter className="justify-end gap-2">
				<Button variant="outline" size="sm" onClick={fn()}>
					Secondary
				</Button>
				<Button size="sm" onClick={fn()}>
					Primary
				</Button>
			</CardFooter>
		</Card>
	),
});

export const ContentOnly = meta.story({
	render: (args) => (
		<Card {...args} className="w-80">
			<CardContent className="text-sm text-muted-foreground">
				{VIDEOS[1].title}
			</CardContent>
		</Card>
	),
});

export const Grid = meta.story({
	render: (args) => (
		<div className="grid w-[min(100%,56rem)] grid-cols-2 gap-4 md:grid-cols-4">
			{CHANNELS.slice(0, 4).map((channel) => (
				<Card key={channel.id} {...args}>
					<CardHeader className="flex-row items-center gap-3">
						<Avatar
							src={channel.profileImageUrl}
							name={channel.displayName}
							size="lg"
						/>
						<CardTitle className="truncate">{channel.displayName}</CardTitle>
					</CardHeader>
					<CardContent className="text-sm text-muted-foreground">
						@{channel.login}
					</CardContent>
				</Card>
			))}
		</div>
	),
});
