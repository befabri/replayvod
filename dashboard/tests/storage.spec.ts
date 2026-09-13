import { expect, test } from "@playwright/test";
import { USER_SETTINGS } from "../src/test/playback-settings";
import { mockTrpc, SESSION, trpcOk } from "./support/trpc";

// Storage readiness end to end: the banner every user sees while the volume
// is away, and the owner's adopt flow that clears it. The status feed is an
// SSE subscription the mock aborts, so the flip comes from the refetch the
// adopt mutation triggers.

const details = (state: string) => ({
	state,
	reason: state === "attached" ? "" : "storage unattached: identity marker .replayvod-storage is missing",
	backend: "local",
	location: "/srv/replayvod/data",
	storage_id: "0123456789abcdef",
	checked_at: "2026-09-08T12:00:00Z",
});

test.describe("storage readiness", () => {
	test("warns while storage is not attached and clears once it is adopted", async ({
		page,
	}) => {
		let state = "unattached";
		let adopts = 0;
		await mockTrpc(page, (procs) => ({
			status: 200,
			body: trpcOk(procs.map((proc) => {
				switch (proc) {
					case "auth.session":
						return SESSION;
					case "settings.get":
						return USER_SETTINGS;
					case "video.listPage":
						return { items: [] };
					case "storage.adopt":
						adopts++;
						state = "attached";
						return { ...details(state), scan_status: "scheduled" };
					case "storage.status":
						return { state, checked_at: details(state).checked_at };
					case "storage.details":
						return details(state);
					default:
						return null;
				}
			})),
		}));

		await page.goto("/dashboard");
		const banner = page.getByTestId("storage-banner");
		await expect(banner).toBeVisible({ timeout: 30_000 });
		await expect(banner).toContainText("Storage is not attached");
		await expect(banner).toContainText("identity marker");

		await banner.getByRole("link", { name: "Open storage settings" }).click();
		await expect(page).toHaveURL(/\/dashboard\/system\/storage$/);
		await expect(page.getByTestId("storage-state")).toHaveText("Not attached");
		await expect(page.getByText("/srv/replayvod/data")).toBeVisible();

		await page.getByRole("button", { name: "Adopt this storage" }).click();
		const dialog = page.getByRole("dialog");
		await expect(dialog).toContainText("/srv/replayvod/data");
		await expect(dialog).toContainText("removes every recording it does not hold");
		await dialog.getByRole("button", { name: "Adopt" }).click();

		await expect(page.getByTestId("storage-state")).toHaveText("Attached", { timeout: 15_000 });
		await expect(page.getByTestId("storage-banner")).toHaveCount(0);
		expect(adopts).toBe(1);
	});

	test("viewers see the warning without the owner details", async ({ page }) => {
		await mockTrpc(page, (procs) => ({
			status: 200,
			body: trpcOk(procs.map((proc) => {
				switch (proc) {
					case "auth.session":
						return { user_id: "u2", login: "bob", display_name: "Bob", email: "", profile_image_url: "", role: "viewer" };
					case "settings.get":
						return { ...USER_SETTINGS, user_id: "u2" };
					case "video.listPage":
						return { items: [] };
					case "storage.status":
						return { state: "unreachable", checked_at: "2026-09-08T12:00:00Z" };
					default:
						return null;
				}
			})),
		}));
		await page.goto("/dashboard");
		const banner = page.getByTestId("storage-banner");
		await expect(banner).toBeVisible({ timeout: 30_000 });
		await expect(banner).toContainText("recordings cannot play");
		await expect(banner.getByRole("link", { name: "Open storage settings" })).toHaveCount(0);
		await expect(banner).not.toContainText("Reason");
	});
});
