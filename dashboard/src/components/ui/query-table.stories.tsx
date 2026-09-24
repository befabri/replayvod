import preview from "#.storybook/preview";
import type { VideoResponse } from "@/api/generated/trpc";
import { makeVideos } from "@/test/fixtures";
import { sampleVideoColumns } from "@/test/sample-columns";
import { QueryTable } from "./query-table";

const VIDEOS = makeVideos(5);

const meta = preview.meta({
	title: "UI/QueryTable",
	component: QueryTable<VideoResponse[], VideoResponse, unknown>,
	args: {
		query: { data: VIDEOS, isLoading: false, error: null },
		columns: sampleVideoColumns,
		getRows: (rows) => rows,
		getRowId: (row) => String(row.id),
		emptyMessage: "Nothing here yet.",
		errorLabel: "Failed to load",
	},
	argTypes: {
		columns: { control: false },
	},
});

export const Data = meta.story();

export const Loading = meta.story({
	args: { query: { data: undefined, isLoading: true, error: null } },
});

export const ErrorState = meta.story({
	name: "Error",
	args: {
		query: {
			data: undefined,
			isLoading: false,
			error: { message: "connection refused" },
		},
	},
});

export const Empty = meta.story({
	args: { query: { data: [], isLoading: false, error: null } },
});
