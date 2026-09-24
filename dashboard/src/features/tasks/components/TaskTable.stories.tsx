import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import { DataTable } from "@/components/ui/data-table";
import i18n from "@/i18n";
import { makeTasks } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { TableSkeletonComparison, tableIn } from "@/test/table-layout";
import { taskColumns } from "./columns";

const TASKS = makeTasks();

function useTaskColumns() {
	const { t } = useTranslation();
	return useMemo(() => taskColumns(t), [t]);
}

const meta = preview.meta({
	title: "Features/Tasks/TaskTable",
	parameters: { layout: "padded" },
});

export const Loaded = meta.story({
	render: function Render() {
		return <DataTable columns={useTaskColumns()} data={TASKS} />;
	},
});

export const Loading = meta.story({
	render: function Render() {
		return <DataTable columns={useTaskColumns()} data={[]} loading />;
	},
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

// A loading row stands where a task without an error will be: its name,
// description and interval lines, its badge and both action buttons.
export const SkeletonMatchesRows = meta.story({
	render: function Render() {
		return (
			<TableSkeletonComparison
				columns={useTaskColumns()}
				rows={TASKS.filter((task) => !task.last_error)}
			/>
		);
	},
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
