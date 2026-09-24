import type { StoryContext } from "@storybook/react-vite";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { expect, fn, screen, waitFor, within } from "storybook/test";
import preview from "#.storybook/preview";
import type { UpdateUserRoleInput } from "@/api/generated/trpc";
import i18n from "@/i18n";
import { CURRENT_USER, makeUserInfos, USERS } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { TableSkeletonComparison, tableIn } from "@/test/table-layout";
import { neverResolves, trpcError, trpcParameters } from "@/test/trpc-mock";
import { userColumns } from "./columns";
import { UsersTable } from "./UsersTable";

const [, VIEWER, OWNER] = USERS;

const updateUserRole = fn(({ user_id, role }: UpdateUserRoleInput) => {
	const user = makeUserInfos("owner").find((u) => u.id === user_id);
	if (!user) throw new Error(`unknown user ${user_id}`);
	return { ...user, role: role as typeof user.role };
});

type PlayContext = Pick<StoryContext, "canvas" | "userEvent">;

function roleSelect({ canvas }: Pick<PlayContext, "canvas">, name: string) {
	const row = canvas.getByRole("row", { name: new RegExp(name) });
	return within(row).getByRole("combobox", {
		name: i18n.t("users.col_role"),
	});
}

async function openRoleOptions(context: PlayContext, name: string) {
	await context.userEvent.click(roleSelect(context, name));
	const listbox = await screen.findByRole("listbox", {
		name: i18n.t("users.col_role"),
	});
	return within(listbox)
		.getAllByRole("option")
		.map((option) => option.textContent);
}

const meta = preview.meta({
	title: "Features/Users/UsersTable",
	component: UsersTable,
	parameters: {
		layout: "padded",
		...trpcParameters({
			system: { listUsers: () => makeUserInfos("owner"), updateUserRole },
		}),
	},
	globals: { role: "owner" },
});

export const OwnerView = meta.story({
	play: async (context) => {
		await waitFor(() =>
			expect(roleSelect(context, OWNER.displayName)).toBeEnabled(),
		);
		await expect(await openRoleOptions(context, VIEWER.displayName)).toEqual([
			i18n.t("users.role_viewer"),
			i18n.t("users.role_admin"),
			i18n.t("users.role_owner"),
		]);
	},
});

export const AdminView = meta.story({
	globals: { role: "admin" },
	parameters: trpcParameters({
		system: { listUsers: () => makeUserInfos("admin") },
	}),
	play: async (context) => {
		await waitFor(() =>
			expect(roleSelect(context, OWNER.displayName)).toBeDisabled(),
		);
		await expect(await openRoleOptions(context, VIEWER.displayName)).toEqual([
			i18n.t("users.role_viewer"),
			i18n.t("users.role_admin"),
		]);
	},
});

export const ChangeRole = meta.story({
	play: async (context) => {
		await waitFor(() =>
			expect(roleSelect(context, VIEWER.displayName)).toBeEnabled(),
		);
		await context.userEvent.click(roleSelect(context, VIEWER.displayName));
		await context.userEvent.click(
			await screen.findByRole("option", { name: i18n.t("users.role_admin") }),
		);
		await waitFor(() =>
			expect(updateUserRole).toHaveBeenCalledWith({
				user_id: VIEWER.id,
				role: "admin",
			}),
		);
	},
});

export const Loading = meta.story({
	parameters: trpcParameters({ system: { listUsers: neverResolves } }),
});

// A loading row holds the avatar and the role picker, the tallest cells, so
// the table keeps its height when the users arrive.
export const SkeletonMatchesRows = meta.story({
	render: function Render() {
		const { t } = useTranslation();
		const columns = useMemo(() => userColumns(CURRENT_USER.id, true, t), [t]);
		return (
			<TableSkeletonComparison
				columns={columns}
				rows={makeUserInfos("owner")}
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

export const LoadError = meta.story({
	parameters: trpcParameters({
		system: {
			listUsers: () => {
				throw trpcError("INTERNAL_SERVER_ERROR", 500, "database is locked");
			},
		},
	}),
	play: async ({ canvas }) => {
		await waitFor(() =>
			expect(canvas.getByRole("alert")).toHaveTextContent(
				i18n.t("users.failed_to_load"),
			),
		);
	},
});
