import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import { DataTable } from "@/components/ui/data-table";
import i18n from "@/i18n";
import { makeSessions } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { TableSkeletonComparison, tableIn } from "@/test/table-layout";
import { sessionColumns } from "./columns";

const SESSIONS = makeSessions();

function useSessionColumns() {
	const { t } = useTranslation();
	return useMemo(() => sessionColumns(t), [t]);
}

const meta = preview.meta({
	title: "Features/Sessions/SessionTable",
	parameters: { layout: "padded" },
});

export const Loaded = meta.story({
	render: function Render() {
		return <DataTable columns={useSessionColumns()} data={SESSIONS} />;
	},
});

export const Loading = meta.story({
	render: function Render() {
		return <DataTable columns={useSessionColumns()} data={[]} loading />;
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

export const SkeletonMatchesRows = meta.story({
	render: function Render() {
		return (
			<TableSkeletonComparison columns={useSessionColumns()} rows={SESSIONS} />
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
