import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import { DataTable } from "@/components/ui/data-table";
import i18n from "@/i18n";
import { makeWhitelistEntries } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { TableSkeletonComparison, tableIn } from "@/test/table-layout";
import { whitelistColumns } from "./columns";

const ENTRIES = makeWhitelistEntries();

function useWhitelistColumns() {
	const { t } = useTranslation();
	return useMemo(() => whitelistColumns(t), [t]);
}

const meta = preview.meta({
	title: "Features/Whitelist/WhitelistTable",
	parameters: { layout: "padded" },
});

export const Loaded = meta.story({
	render: function Render() {
		return <DataTable columns={useWhitelistColumns()} data={ENTRIES} />;
	},
});

export const Loading = meta.story({
	render: function Render() {
		return <DataTable columns={useWhitelistColumns()} data={[]} loading />;
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
			<TableSkeletonComparison columns={useWhitelistColumns()} rows={ENTRIES} />
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
