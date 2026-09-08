// @vitest-environment jsdom

import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createElement } from "react";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InviteActions } from "./columns";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("sonner", () => ({
	toast: { success: vi.fn(), error: vi.fn() },
}));
// The wire submission and the copy-once panel are under test; the
// invites table is not, but its row actions are captured so the test can
// drive the panel the way a row would.
vi.mock("@/components/ui/query-table", () => ({
	QueryTable: () => null,
}));
const table = vi.hoisted(() => ({
	actions: undefined as InviteActions | undefined,
}));
vi.mock("@/features/invites/components/columns", () => ({
	inviteColumns: (_t: unknown, actions: InviteActions) => {
		table.actions = actions;
		return [];
	},
}));
const create = {
	mutateAsync: vi.fn(async () => ({}) as { id: number; url: string }),
	isPending: false,
	isError: false,
	error: null as Error | null,
};
vi.mock("@/features/invites", () => ({
	useInvites: () => ({ data: [], isLoading: false, isError: false }),
	useCreateInvite: () => create,
}));

import { InvitesSection } from "./InvitesSection";

const URL = "https://dash.example/invite/raw-token";

beforeEach(() => {
	create.mutateAsync.mockClear();
	create.isError = false;
	create.error = null;
	table.actions = undefined;
	vi.clearAllMocks();
});
afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

async function choose(combobox: string, option: string) {
	fireEvent.click(screen.getByRole("combobox", { name: combobox }));
	const item = await screen.findByRole("option", { name: option });
	fireEvent.pointerDown(item, { pointerType: "mouse", button: 0 });
	fireEvent.click(item);
	await waitFor(() =>
		expect(
			screen.getByRole("combobox", { name: combobox }).textContent,
		).toContain(option),
	);
}

async function fillAndSubmit({ role, note }: { role?: string; note?: string }) {
	render(createElement(InvitesSection));
	if (role) await choose("invites.col_role", `users.role_${role}`);
	await choose("invites.field_ttl", "invites.ttl_1h");
	if (note !== undefined) {
		fireEvent.change(screen.getByLabelText("invites.col_note"), {
			target: { value: note },
		});
	}
	fireEvent.click(screen.getByRole("button", { name: "invites.create" }));
}

describe("InvitesSection create flow", () => {
	it("submits ttl_minutes as a number and the note trimmed", async () => {
		await fillAndSubmit({ role: "admin", note: "  for bob  " });
		await waitFor(() => {
			expect(create.mutateAsync).toHaveBeenCalledWith({
				role: "admin",
				ttl_minutes: 60,
				note: "for bob",
			});
		});
	});

	it("drops an empty note instead of sending it", async () => {
		await fillAndSubmit({ note: "   " });
		await waitFor(() => {
			expect(create.mutateAsync).toHaveBeenCalledWith({
				role: "viewer",
				ttl_minutes: 60,
				note: undefined,
			});
		});
	});

	it("shows the copy-once URL panel when the invite was created", async () => {
		create.mutateAsync.mockResolvedValueOnce({ id: 7, url: URL });
		await fillAndSubmit({});
		await waitFor(() => {
			expect(screen.getByText("invites.url_ready")).toBeTruthy();
		});
		expect(screen.getByText(URL)).toBeTruthy();
		expect(screen.getByRole("button", { name: "invites.copy" })).toBeTruthy();
	});

	it("keeps the form usable after a rejected create and lets the admin retry", async () => {
		create.mutateAsync.mockRejectedValueOnce(new Error("create unavailable"));
		await fillAndSubmit({ role: "admin", note: "for bob" });
		await waitFor(() => {
			expect(create.mutateAsync).toHaveBeenCalledTimes(1);
			expect(
				screen.getByRole<HTMLButtonElement>("button", {
					name: "invites.create",
				}).disabled,
			).toBe(false);
		});
		expect(
			screen.getByLabelText<HTMLInputElement>("invites.col_note").value,
		).toBe("for bob");
		expect(screen.queryByText("invites.url_ready")).toBeNull();
		fireEvent.click(screen.getByRole("button", { name: "invites.create" }));
		await waitFor(() => expect(create.mutateAsync).toHaveBeenCalledTimes(2));
		expect(create.mutateAsync).toHaveBeenLastCalledWith({
			role: "admin",
			ttl_minutes: 60,
			note: "for bob",
		});
	});

	it.each([
		false,
		true,
	])("reports clipboard failure=%s when copying the returned URL", async (fail) => {
		const writeText = vi.fn(async () => {
			if (fail) throw new Error("clipboard unavailable");
		});
		vi.stubGlobal("navigator", { clipboard: { writeText } });
		create.mutateAsync.mockResolvedValueOnce({ id: 7, url: URL });
		await fillAndSubmit({});
		fireEvent.click(
			await screen.findByRole("button", { name: "invites.copy" }),
		);
		await waitFor(() => {
			expect(writeText).toHaveBeenCalledWith(URL);
			expect(fail ? toast.error : toast.success).toHaveBeenCalledWith(
				fail ? "invites.url_copy_failed" : "invites.url_copied",
			);
		});
		expect(screen.getByText(URL)).toBeTruthy();
	});
});

describe("InvitesSection table-issued links", () => {
	it("shows a link issued from a row and drops it once that invite is revoked", () => {
		render(createElement(InvitesSection));
		expect(screen.queryByText("invites.url_ready")).toBeNull();
		const fresh = "https://dash.example/invite/fresh-token";
		act(() => {
			table.actions?.onIssued({
				id: 7,
				url: fresh,
				role: "viewer",
				expires_at: "2026-06-02T12:00:00Z",
			});
		});
		expect(screen.getByText(fresh)).toBeTruthy();
		act(() => table.actions?.onRevoked(8));
		expect(screen.getByText(fresh)).toBeTruthy();
		act(() => table.actions?.onRevoked(7));
		expect(screen.queryByText("invites.url_ready")).toBeNull();
	});

	it("replaces the created link with the one issued from a row", async () => {
		create.mutateAsync.mockResolvedValueOnce({ id: 7, url: URL });
		await fillAndSubmit({});
		expect(await screen.findByText(URL)).toBeTruthy();
		const fresh = "https://dash.example/invite/fresh-token";
		act(() => {
			table.actions?.onIssued({
				id: 9,
				url: fresh,
				role: "admin",
				expires_at: "2026-06-02T12:00:00Z",
			});
		});
		expect(screen.getByText(fresh)).toBeTruthy();
		expect(screen.queryByText(URL)).toBeNull();
	});
});
