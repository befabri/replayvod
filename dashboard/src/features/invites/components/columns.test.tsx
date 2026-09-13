// @vitest-environment jsdom

import type { CellContext } from "@tanstack/react-table";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { TFunction } from "i18next";
import type { ReactElement } from "react";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InviteInfo } from "@/features/invites";

vi.mock("sonner", () => ({
	toast: { success: vi.fn(), error: vi.fn() },
}));
const rotate = { mutate: vi.fn(), isPending: false };
const revoke = { mutate: vi.fn(), isPending: false };
vi.mock("@/features/invites", async (importOriginal) => ({
	...(await importOriginal<typeof import("@/features/invites")>()),
	useRotateInvite: () => rotate,
	useRevokeInvite: () => revoke,
}));

import { type InviteActions, inviteColumns } from "./columns";

const t = ((key: string) => key) as unknown as TFunction;
const pending: InviteInfo = {
	id: 7,
	role: "viewer",
	created_by: "admin",
	created_at: "2026-06-01T12:00:00Z",
	expires_at: new Date(Date.now() + 3_600_000).toISOString(),
};
const expired: InviteInfo = {
	...pending,
	expires_at: new Date(Date.now() - 1_000).toISOString(),
};
const redeemed: InviteInfo = {
	...pending,
	redeemed_at: "2026-06-01T13:00:00Z",
	redeemed_by: "twitch-99",
};

function renderActions(invite: InviteInfo): InviteActions {
	const actions = { onIssued: vi.fn(), onRevoked: vi.fn() };
	const column = inviteColumns(t, actions).find((c) => c.id === "actions");
	if (!column || typeof column.cell !== "function") {
		throw new Error("actions column missing");
	}
	render(
		column.cell({
			row: { original: invite },
		} as CellContext<InviteInfo, unknown>) as ReactElement,
	);
	return actions;
}

beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);

describe("invite row actions", () => {
	it("offers a new link only while the invite is still pending", () => {
		renderActions(pending);
		expect(
			screen.getByRole("button", { name: "invites.new_link" }),
		).toBeTruthy();
		expect(screen.getByRole("button", { name: "invites.revoke" })).toBeTruthy();
		cleanup();

		renderActions(expired);
		expect(
			screen.queryByRole("button", { name: "invites.new_link" }),
		).toBeNull();
		expect(screen.getByRole("button", { name: "invites.revoke" })).toBeTruthy();
		cleanup();

		renderActions(redeemed);
		expect(screen.queryByRole("button")).toBeNull();
	});

	it("hands the fresh link to the section and reports a rejected rotate", () => {
		const actions = renderActions(pending);
		fireEvent.click(screen.getByRole("button", { name: "invites.new_link" }));
		expect(rotate.mutate).toHaveBeenCalledTimes(1);
		const [input, options] = rotate.mutate.mock.calls[0];
		expect(input).toEqual({ id: 7 });
		const issued = {
			id: 7,
			url: "https://dash.example/invite/fresh-token",
			role: "viewer",
			expires_at: pending.expires_at,
		};
		options.onSuccess(issued);
		expect(actions.onIssued).toHaveBeenCalledWith(issued);
		options.onError(new Error("invite not found"));
		expect(toast.error).toHaveBeenCalledWith("invites.failed_to_rotate");
	});

	it("locks the new link button while a rotate is in flight", () => {
		rotate.isPending = true;
		try {
			renderActions(pending);
			expect(
				screen.getByRole<HTMLButtonElement>("button", {
					name: "invites.new_link",
				}).disabled,
			).toBe(true);
		} finally {
			rotate.isPending = false;
		}
	});

	it.each([
		"rotate",
		"revoke",
	])("blocks both actions while %s is in flight", (pendingAction) => {
		const mutation = pendingAction === "rotate" ? rotate : revoke;
		mutation.isPending = true;
		try {
			renderActions(pending);
			for (const name of ["invites.new_link", "invites.revoke"]) {
				const button = screen.getByRole<HTMLButtonElement>("button", { name });
				expect(button.disabled).toBe(true);
				fireEvent.click(button);
			}
			expect(rotate.mutate).not.toHaveBeenCalled();
			expect(revoke.mutate).not.toHaveBeenCalled();
		} finally {
			mutation.isPending = false;
		}
	});

	it("tells the section which invite was revoked", () => {
		const actions = renderActions(pending);
		fireEvent.click(screen.getByRole("button", { name: "invites.revoke" }));
		expect(revoke.mutate).toHaveBeenCalledTimes(1);
		const [input, options] = revoke.mutate.mock.calls[0];
		expect(input).toEqual({ id: 7 });
		options.onSuccess();
		expect(actions.onRevoked).toHaveBeenCalledWith(7);
	});
});
