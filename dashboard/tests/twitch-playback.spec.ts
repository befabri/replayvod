import { expect, test, type Page } from "@playwright/test";
import { procsOf, SESSION, trpcOk } from "./support/trpc";

// Stop outstanding alternate-origin streams before Playwright closes the page.
test.afterEach(async ({ page }) => {
	await page.unrouteAll({ behavior: "ignoreErrors" });
});

const token = "test-session-0123456789abcdef";
type Connection = {
	state: string;
	login: string;
	checked_at: number;
	expires_at: number;
};
const disconnected: Connection = {
	state: "disconnected",
	login: "",
	checked_at: 0,
	expires_at: 0,
};

async function playbackServer(page: Page, initial = disconnected) {
	const state = {
		connection: { ...initial },
		connectCalls: 0,
		rejectConnect: false,
		checkUnavailable: false,
		revoked: false,
	};
	await page.route("**/trpc/**", async (route) => {
		const req = route.request();
		if ((req.headers().accept ?? "").includes("text/event-stream")) {
			await route.abort();
			return;
		}
		const procs = procsOf(req.url());
		const values = procs.map((proc) => {
			if (proc === "auth.session") return SESSION;
			if (proc === "twitchPlayback.connect") {
				state.connectCalls++;
				expect(req.method()).toBe("POST");
				expect(req.url()).not.toContain(token);
				expect(Object.values(req.postDataJSON())).toContainEqual({
					session_token: token,
					consent: true,
				});
				if (!state.rejectConnect)
					state.connection = {
						...disconnected,
						state: "connected",
						login: "alice",
					};
			}
			if (proc === "twitchPlayback.check" && state.revoked)
				state.connection.state = "reconnect_required";
			if (proc === "twitchPlayback.disconnect")
				state.connection = { ...disconnected };
			if (proc.startsWith("twitchPlayback.")) return { ...state.connection };
			return null;
		});
		const reject =
			(state.rejectConnect && procs.includes("twitchPlayback.connect")) ||
			(state.checkUnavailable && procs.includes("twitchPlayback.check"));
		const status = state.checkUnavailable ? 503 : 400;
		const body = reject
			? procs.map(() => ({
					error: {
						message: state.checkUnavailable
							? "Twitch validation temporarily unavailable"
							: "Twitch session expired; reconnect",
						code: -32603,
						data: {
							code: state.checkUnavailable
								? "SERVICE_UNAVAILABLE"
								: "BAD_REQUEST",
							httpStatus: status,
						},
					},
				}))
			: trpcOk(values);
		expect(JSON.stringify(body)).not.toContain(token);
		await route.fulfill({
			status: reject ? status : 200,
			contentType: "application/json",
			body: JSON.stringify(body),
		});
	});
	return state;
}

async function submitConnection(page: Page) {
	const input = page.getByLabel("Twitch auth-token cookie value");
	await input.fill(token);
	await page.getByRole("checkbox").check();
	await page
		.getByRole("button", { name: /Validate and connect|Validate and replace/ })
		.click();
	await expect(input).toHaveValue("");
}

// alternateOrigin serves the real app at a second browser origin, which
// exercises secure-context behaviour without DNS, certificates or real secrets.
async function alternateOrigin(page: Page, origin: string, baseURL: string) {
	await page.route(`${origin}/**`, async (route) => {
		const url = new URL(route.request().url());
		const response = await route.fetch({
			url: new URL(url.pathname + url.search, baseURL).href,
		});
		await route.fulfill({ response });
	});
}

for (const https of [false, true]) {
	test(`owner connects, checks and disconnects over ${https ? "HTTPS" : "loopback HTTP"}`, async ({
		page,
		baseURL,
	}) => {
		const origin = https ? "https://replayvod.test" : baseURL!;
		if (https) await alternateOrigin(page, origin, baseURL!);
		const state = await playbackServer(page);
		await page.goto(`${origin}/dashboard/system/twitch`);
		await expect(
			page.getByText("Anonymous playback", { exact: true }),
		).toBeVisible();
		await page.getByText("Where to find your Twitch session").click();
		await expect(page.getByText(/In Firefox or Zen/)).toBeVisible();
		const input = page.getByLabel("Twitch auth-token cookie value");
		await input.fill(token);
		await expect(
			page.getByRole("button", { name: "Validate and connect" }),
		).toBeDisabled();
		expect(state.connectCalls).toBe(0);
		await submitConnection(page);
		await expect(page.getByText("Connected", { exact: true })).toBeVisible();
		await page.reload();
		await expect(page.getByText("Connected", { exact: true })).toBeVisible();
		await expect(input).toHaveValue("");
		await page.getByRole("button", { name: "Check connection" }).click();
		await expect(
			page.getByText("Connection checked.", { exact: true }),
		).toBeVisible();
		await page.getByRole("button", { name: "Disconnect", exact: true }).click();
		await expect(
			page.getByText("Anonymous playback", { exact: true }),
		).toBeVisible();
		expect(
			await page.evaluate(() =>
				JSON.stringify({
					local: { ...localStorage },
					session: { ...sessionStorage },
				}),
			),
		).not.toContain(token);
	});
}

test("remote HTTP blocks credential entry and forced submission but still permits disconnect", async ({
	page,
	baseURL,
}) => {
	const origin = "http://replayvod.test";
	await alternateOrigin(page, origin, baseURL!);
	const state = await playbackServer(page, {
		...disconnected,
		state: "connected",
		login: "alice",
	});
	await page.goto(`${origin}/dashboard/system/twitch`);
	await expect(page.getByText(/Use HTTPS for both/)).toBeVisible();
	const input = page.getByLabel("Twitch auth-token cookie value");
	await expect(input).toBeDisabled();
	await expect(page.getByRole("checkbox")).toBeDisabled();
	await input.evaluate((element) =>
		element
			.closest("form")!
			.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
	);
	await expect(input).toHaveValue("");
	expect(state.connectCalls).toBe(0);
	await page.getByRole("button", { name: "Disconnect", exact: true }).click();
	await expect(
		page.getByText("Anonymous playback", { exact: true }),
	).toBeVisible();
});

test("failed checks and replacements preserve the account; revoked sessions can be reconnected", async ({
	page,
}) => {
	const state = await playbackServer(page, {
		...disconnected,
		state: "connected",
		login: "alice",
	});
	await page.goto("/dashboard/system/twitch");
	await expect(page.getByText("Connected", { exact: true })).toBeVisible();
	state.checkUnavailable = true;
	await page.getByRole("button", { name: "Check connection" }).click();
	await expect(page.getByRole("alert")).toContainText(
		"temporarily unavailable",
	);
	await expect(page.getByText("Connected", { exact: true })).toBeVisible();
	state.checkUnavailable = false;
	state.rejectConnect = true;
	await submitConnection(page);
	await expect(page.getByRole("alert")).toContainText("expired");
	await expect(page.getByText("Connected", { exact: true })).toBeVisible();
	state.rejectConnect = false;
	state.revoked = true;
	await page.getByRole("button", { name: "Check connection" }).click();
	await expect(
		page.getByText("Reconnect required", { exact: true }),
	).toBeVisible();
	state.revoked = false;
	await submitConnection(page);
	await expect(page.getByText("Connected", { exact: true })).toBeVisible();
});
