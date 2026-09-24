import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { DataTable } from "@/components/ui/data-table";
import i18n from "@/i18n";
import { makeInvites } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { TableSkeletonComparison, tableIn } from "@/test/table-layout";
import { inviteColumns } from "./columns";

const INVITES = makeInvites();
const ACTIONS = { onIssued: fn(), onRevoked: fn() };

function useInviteColumns() {
	const { t } = useTranslation();
	return useMemo(() => inviteColumns(t, ACTIONS), [t]);
}

const meta = preview.meta({
	title: "Features/Invites/InviteTable",
	parameters: { layout: "padded" },
});

export const Loaded = meta.story({
	render: function Render() {
		return <DataTable columns={useInviteColumns()} data={INVITES} />;
	},
});

export const Loading = meta.story({
	render: function Render() {
		return <DataTable columns={useInviteColumns()} data={[]} loading />;
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
			<TableSkeletonComparison columns={useInviteColumns()} rows={INVITES} />
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
