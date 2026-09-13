import { expect, test } from "@playwright/test";
import {
	mockSubscription,
	mockTrpc,
	trpcOk,
	validSession,
} from "./support/trpc";

// History against a recording whose files left storage: it sits under the
// Unavailable filter with Restore and permanent removal, and restoring it
// hands the row back to the library.

const missing = {
	id: 95,
	job_id: "job-95",
	filename: "gone",
	display_name: "Streamer",
	broadcaster_id: "u1",
	broadcaster_login: "streamer",
	broadcaster_name: "Streamer",
	title: "The one that left",
	status: "DONE",
	completion_kind: "complete",
	truncated: false,
	quality: "HIGH",
	is_audio_only: false,
	viewer_count: 0,
	language: "en",
	duration_seconds: 600,
	size_bytes: 1000,
	start_download_at: "2026-09-01T10:00:00Z",
	downloaded_at: "2026-09-01T11:00:00Z",
	deleted_at: "2026-09-06T16:00:00Z",
	deletion_kind: "missing",
	source: "live",
};

test.describe("history restore", () => {
	test("lists a file-missing recording under Unavailable and restores it", async ({
		page,
	}) => {
		let restored = false;
		const listInputs: string[] = [];
		await mockTrpc(page, (procs, url) => {
			const session = validSession(procs, url);
			if (session) return session;
			if (procs.includes("video.restore")) {
				restored = true;
				return { status: 200, body: trpcOk(procs.map(() => ({ ok: true }))) };
			}
			if (procs.includes("video.listPage")) {
				listInputs.push(new URL(url).searchParams.get("input") ?? "");
				return {
					status: 200,
					body: trpcOk(
						procs.map((p) =>
							p === "video.listPage"
								? { items: restored ? [] : [missing], next_cursor: null }
								: null,
						),
					),
				};
			}
			if (procs.includes("video.historyCounts")) {
				const n = restored ? 0 : 1;
				const counts = { on_disk: 0, removed: n, unavailable: n };
				return {
					status: 200,
					body: trpcOk(
						procs.map((p) =>
							p === "video.historyCounts"
								? {
										all: counts,
										completed: counts,
										failed: { on_disk: 0, removed: 0, unavailable: 0 },
										cancelled: { on_disk: 0, removed: 0, unavailable: 0 },
									}
								: null,
						),
					),
				};
			}
			return null;
		});

		await page.goto(
			"/dashboard/activity/history?outcome=all&media=unavailable",
		);
		const row = page.getByRole("row", { name: /The one that left/ });
		await expect(row).toBeVisible({ timeout: 30_000 });
		await expect(row).toContainText("Missing");
		expect(listInputs.at(-1)).toContain('"scope":"removed"');
		expect(listInputs.at(-1)).toContain('"deletion_kind":"missing"');

		await row.getByRole("button", { name: "Restore" }).click();
		await expect(page.getByText("Recording restored")).toBeVisible();
		await expect(row).toHaveCount(0);
		await expect(
			page.getByText("No recording is waiting for its files."),
		).toBeVisible();
		expect(restored).toBe(true);
	});

	test("offers permanent removal with its own warning", async ({ page }) => {
		await mockTrpc(page, (procs, url) => {
			const session = validSession(procs, url);
			if (session) return session;
			if (procs.includes("video.listPage")) {
				return {
					status: 200,
					body: trpcOk(
						procs.map((p) =>
							p === "video.listPage"
								? { items: [missing], next_cursor: null }
								: null,
						),
					),
				};
			}
			return null;
		});
		await page.goto("/dashboard/activity/history?outcome=all&media=removed");
		const row = page.getByRole("row", { name: /The one that left/ });
		await expect(row).toBeVisible({ timeout: 30_000 });
		await row.getByRole("button", { name: "Remove" }).click();
		const dialog = page.getByRole("dialog");
		await expect(dialog).toContainText("can no longer be restored");
	});
});

test("a refused removal explains the failure and can be retried", async ({
	page,
}) => {
	let attempts = 0;
	await mockTrpc(page, (procs, url) => {
		const session = validSession(procs, url);
		if (session) return session;
		if (procs.includes("video.delete")) {
			attempts++;
			return attempts === 1
				? {
						status: 409,
						body: [
							{
								error: {
									message: "Recording is still being finalized",
									code: -32009,
									data: { code: "CONFLICT", httpStatus: 409 },
								},
							},
						],
					}
				: { status: 200, body: trpcOk([{ ok: true }]) };
		}
		return {
			status: 200,
			body: trpcOk(
				procs.map((proc) =>
					proc === "video.listPage"
						? {
								items: [
									{
										...missing,
										delete_requested_at:
											attempts > 1 ? "2026-09-12T12:00:00Z" : undefined,
									},
								],
								next_cursor: null,
							}
						: null,
				),
			),
		};
	});
	await page.goto("/dashboard/activity/history?media=removed");
	await page.getByRole("button", { name: "Remove", exact: true }).click();
	const dialog = page.getByRole("dialog");
	await dialog.getByRole("button", { name: "Remove", exact: true }).click();
	await expect(
		page.getByText("Recording is still being finalized", { exact: true }),
	).toBeVisible();
	await expect(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Remove", exact: true }).click();
	await expect(dialog).toHaveCount(0);
	await expect(page.getByText("Removal queued", { exact: true })).toBeVisible();
	expect(attempts).toBe(2);
});

for (const surface of ["history", "watch"] as const) {
	test(`${surface} follows queued permanent deletion and never offers restore while it runs`, async ({
		page,
	}) => {
		const publishRemoval = await mockSubscription(page, "video.changesLive");
		let phase: "missing" | "pending" | "removed" = "missing";
		let removals = 0;
		let restores = 0;
		await mockTrpc(page, (procs, url) => {
			const session = validSession(procs, url);
			if (session) return session;
			const current = () => ({
				...missing,
				deletion_kind: phase === "removed" ? "manual" : "missing",
				delete_requested_at:
					phase === "pending" ? "2026-09-12T12:00:00Z" : undefined,
			});
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) => {
						if (proc === "video.delete") {
							phase = "pending";
							removals++;
							return { ok: true };
						}
						if (proc === "video.restore") {
							restores++;
							return { ok: false };
						}
						if (proc === "video.getById") return current();
						if (proc === "video.listPage")
							return { items: [current()], next_cursor: null };
						return null;
					}),
				),
			};
		});
		await page.goto(
			surface === "history"
				? "/dashboard/activity/history?media=removed"
				: "/dashboard/watch/95",
		);
		await page.getByRole("button", { name: "Remove", exact: true }).click();
		await page
			.getByRole("dialog")
			.getByRole("button", { name: "Remove", exact: true })
			.click();
		await expect(
			page.getByText("Removal queued", { exact: true }),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Restore", exact: true }),
		).toHaveCount(0);
		await expect(
			page.getByRole("button", { name: "Remove", exact: true }),
		).toHaveCount(0);
		phase = "removed";
		publishRemoval();
		await expect(page.getByText("Removal queued", { exact: true })).toHaveCount(
			0,
			{ timeout: 10_000 },
		);
		expect(removals).toBe(1);
		expect(restores).toBe(0);
	});
}
