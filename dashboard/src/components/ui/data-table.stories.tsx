import { type ColumnDef, functionalUpdate } from "@tanstack/react-table";
import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import type { VideoResponse } from "@/api/generated/trpc";
import i18n from "@/i18n";
import { makeVideos } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { sampleVideoColumns } from "@/test/sample-columns";
import { useStoryArg } from "@/test/story-args";
import { TableSkeletonComparison, tableIn } from "@/test/table-layout";
import { Checkbox } from "./checkbox";
import { DataTable } from "./data-table";

const VIDEOS = makeVideos(8);
const MANY_VIDEOS = makeVideos(2000);
const SELECTABLE = VIDEOS.filter((video) => !video.is_audio_only);

const SELECTION_COLUMN: ColumnDef<VideoResponse> = {
	id: "select",
	header: ({ table }) => (
		<Checkbox
			aria-label="Select all rows"
			checked={table.getIsAllRowsSelected()}
			disabled={!table.getRowModel().rows.some((row) => row.getCanSelect())}
			onCheckedChange={(checked) =>
				table.toggleAllRowsSelected(checked === true)
			}
		/>
	),
	cell: ({ row }) =>
		row.getCanSelect() ? (
			<Checkbox
				aria-label={`Select ${row.original.title}`}
				checked={row.getIsSelected()}
				onCheckedChange={(checked) => row.toggleSelected(checked === true)}
			/>
		) : null,
};

const meta = preview.meta({
	title: "UI/DataTable",
	component: DataTable<VideoResponse, unknown>,
	args: {
		columns: sampleVideoColumns,
		data: VIDEOS,
		getRowId: (row) => String(row.id),
	},
	argTypes: {
		columns: { control: false },
		data: { control: false },
	},
});

export const Default = meta.story();

export const Sorted = meta.story({
	args: { sorting: [{ id: "duration_seconds", desc: true }] },
	render: function Render(args, context) {
		const [sorting, setSorting] = useStoryArg(args, "sorting", context);
		return (
			<DataTable
				{...args}
				sorting={sorting}
				onSortingChange={(updater) =>
					setSorting(functionalUpdate(updater, sorting ?? []))
				}
			/>
		);
	},
});

export const Empty = meta.story({
	args: {
		data: [],
		emptyMessage: "Nothing here yet.",
	},
});

export const Loading = meta.story({
	args: { data: [], loading: true, loadingRows: 6 },
	play: async ({ canvas }) => {
		await expect(canvas.getByRole("table")).toHaveAttribute(
			"aria-busy",
			"true",
		);
		await expect(canvas.getByRole("status")).toHaveTextContent(
			i18n.t("common.loading"),
		);
	},
});

// A column without its own skeleton gets a bar on a text line, so a table of
// plain text cells keeps its row height while it loads.
export const SkeletonMatchesTextRows = meta.story({
	render: () => (
		<TableSkeletonComparison
			columns={sampleVideoColumns.filter((column) =>
				["Title", "Duration", "Size", "Recorded"].includes(
					String(column.header),
				),
			)}
			rows={VIDEOS.slice(0, 5)}
		/>
	),
	play: async ({ canvas }) => {
		await expect(
			layoutMismatches(
				tableIn(canvas.getByTestId("skeleton")),
				tableIn(canvas.getByTestId("loaded")),
				{ axis: "vertical" },
			),
		).toEqual([]);
	},
});

export const RowSelection = meta.story({
	args: {
		columns: [SELECTION_COLUMN, ...sampleVideoColumns],
		enableRowSelection: (row) => !row.original.is_audio_only,
		rowSelection: { [String(SELECTABLE[0].id)]: true },
	},
	render: function Render(args, context) {
		const [rowSelection, setRowSelection] = useStoryArg(
			args,
			"rowSelection",
			context,
		);
		const selection = rowSelection ?? {};
		const selected = Object.keys(selection).filter((id) => selection[id]);
		return (
			<div className="space-y-3">
				<p className="text-sm text-muted-foreground">
					{selected.length} of {args.data.length} selected
				</p>
				<DataTable
					{...args}
					rowSelection={selection}
					onRowSelectionChange={(updater) =>
						setRowSelection(functionalUpdate(updater, selection))
					}
				/>
			</div>
		);
	},
	play: async ({ canvas, userEvent }) => {
		const next = canvas.getByRole("checkbox", {
			name: `Select ${SELECTABLE[1].title}`,
		});
		await userEvent.click(next);
		await expect(next).toHaveAttribute("aria-checked", "true");
		await expect(
			canvas.getByText(`2 of ${VIDEOS.length} selected`),
		).toBeVisible();
	},
});

export const Virtualized = meta.story({
	args: {
		data: MANY_VIDEOS,
		virtualizeRows: true,
		estimateRowHeight: 53,
	},
});
