import preview from "#.storybook/preview";
import { FIXTURE_NOW } from "@/test/fixtures";
import { Timestamp } from "./timestamp";

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

function ago(ms: number) {
	return new Date(FIXTURE_NOW - ms).toISOString();
}

const RANGES = [
	{ label: "Just now", iso: ago(12 * SECOND) },
	{ label: "Minutes ago", iso: ago(7 * MINUTE) },
	{ label: "Hours ago", iso: ago(5 * HOUR) },
	{ label: "Days ago", iso: ago(3 * DAY) },
	{ label: "Older than a week", iso: ago(23 * DAY) },
];

const meta = preview.meta({
	title: "UI/Timestamp",
	component: Timestamp,
	args: { iso: ago(7 * MINUTE) },
});

export const JustNow = meta.story({
	args: { iso: ago(12 * SECOND) },
});

export const MinutesAgo = meta.story({
	args: { iso: ago(7 * MINUTE) },
});

export const HoursAgo = meta.story({
	args: { iso: ago(5 * HOUR) },
});

export const DaysAgo = meta.story({
	args: { iso: ago(3 * DAY) },
});

export const OlderThanAWeek = meta.story({
	args: { iso: ago(23 * DAY) },
});

export const AllRanges = meta.story({
	argTypes: { iso: { control: false } },
	render: (args) => (
		<dl className="grid w-fit grid-cols-[auto_auto] gap-x-6 gap-y-2 text-sm">
			{RANGES.map((range) => (
				<div key={range.label} className="contents">
					<dt className="text-muted-foreground">{range.label}</dt>
					<dd>
						<Timestamp {...args} iso={range.iso} />
					</dd>
				</div>
			))}
		</dl>
	),
});
