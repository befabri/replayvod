import type { ComponentProps } from "react";
import preview from "#.storybook/preview";
import { allOf } from "@/test/exhaustive";
import { CHANNELS } from "@/test/fixtures";
import { Avatar } from "./avatar";

type Size = NonNullable<ComponentProps<typeof Avatar>["size"]>;

const SIZES = allOf<Size>({
	sm: true,
	md: true,
	lg: true,
	xl: true,
	"2xl": true,
	"3xl": true,
});

const meta = preview.meta({
	title: "UI/Avatar",
	component: Avatar,
	args: {
		src: CHANNELS[0].profileImageUrl,
		name: CHANNELS[0].displayName,
		size: "xl" as const,
	},
	argTypes: {
		size: { control: "select", options: SIZES },
	},
});

export const Default = meta.story();

export const Initials = meta.story({
	args: { src: null, name: "Moonlit Marmot" },
});

export const BrokenImage = meta.story({
	args: {
		src: "data:image/png;base64,bm90LWFuLWltYWdl",
		name: CHANNELS[5].displayName,
	},
});

export const Live = meta.story({
	args: { isLive: true },
});

export const Sizes = meta.story({
	render: (args) => (
		<div className="flex flex-col gap-4">
			<div className="flex items-end gap-3">
				{SIZES.map((size) => (
					<Avatar key={size} {...args} size={size} />
				))}
			</div>
			<div className="flex items-end gap-3">
				{SIZES.map((size) => (
					<Avatar key={size} {...args} src={null} size={size} />
				))}
			</div>
			<div className="flex items-end gap-3">
				{SIZES.map((size) => (
					<Avatar key={size} {...args} size={size} isLive />
				))}
			</div>
		</div>
	),
});

export const ChannelList = meta.story({
	render: (args) => (
		<ul className="w-72 space-y-3 rounded-lg bg-card p-4 text-sm">
			{CHANNELS.slice(0, 4).map((channel, index) => (
				<li key={channel.id} className="flex items-center gap-3">
					<Avatar
						{...args}
						src={channel.profileImageUrl}
						name={channel.displayName}
						size="md"
						isLive={index % 2 === 0}
					/>
					<span className="font-medium">{channel.displayName}</span>
				</li>
			))}
		</ul>
	),
});
