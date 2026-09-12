import { spawn, type ChildProcess } from "node:child_process";
import { resolve } from "node:path";
import { expect, test } from "@playwright/test";

let server: ChildProcess;
let origin: string;

test.beforeAll(async () => {
	test.setTimeout(180_000);
	server = spawn(
		"go",
		[
			"run",
			"./internal/server/api/subscriptions/testdata/browser.go",
			resolve("dist/client"),
		],
		{
			cwd: resolve("../server"),
			detached: true,
			stdio: ["ignore", "pipe", "pipe"],
		},
	);
	origin = await new Promise<string>((done, reject) => {
		let errors = "";
		const timer = setTimeout(
			() => reject(new Error(`Subscription fixture timed out: ${errors}`)),
			170_000,
		);
		server.once("error", (error) => {
			clearTimeout(timer);
			reject(error);
		});
		server.stderr?.on("data", (data) => {
			errors += String(data);
		});
		server.stdout?.once("data", (data) => {
			clearTimeout(timer);
			done(String(data).trim());
		});
		server.once("exit", (code) => {
			clearTimeout(timer);
			reject(new Error(`Subscription fixture exited ${code}: ${errors}`));
		});
	});
});

test.afterAll(() => {
	if (server?.pid) process.kill(-server.pid, "SIGTERM");
});

test("multiple tabs share their feeds without blocking HTTP and recover after reconnect", async ({
	context,
	request,
}) => {
	const pages = await Promise.all([
		context.newPage(),
		context.newPage(),
		context.newPage(),
	]);
	try {
		await Promise.all(pages.map((page) => page.goto(`${origin}/dashboard`)));
		const stats = async () =>
			(await request.get(`${origin}/test/stats`)).json();
		await expect
			.poll(async () => (await stats()).feeds)
			.toMatchObject({
				"storage.statusLive": 3,
				"stream.status": 3,
				"video.removalsLive": 3,
				"stream.live": 3,
				"video.activeDownloadsLive": 3,
			});
		expect((await stats()).connections).toBe(3);
		// Real browser HTTP requests on the same HTTP/1.1 origin. Fifteen SSE feeds
		// would exhaust its six connections; three multiplexed sockets do not.
		for (const page of pages) {
			expect(
				await page.evaluate(
					async () =>
						(await fetch("/test/stats", { signal: AbortSignal.timeout(3_000) }))
							.status,
				),
			).toBe(200);
		}
		await request.get(`${origin}/test/storage?state=unattached`);
		for (const page of pages)
			await expect(page.getByTestId("storage-banner")).toBeVisible();
		await request.get(`${origin}/test/disconnect?state=attached`);
		await expect
			.poll(async () => (await stats()).feeds["storage.statusLive"])
			.toBe(3);
		for (const page of pages)
			await expect(page.getByTestId("storage-banner")).toHaveCount(0);
		const recordings = pages[0].getByRole("button", {
			name: "Recordings",
			exact: true,
		});
		if ((await recordings.getAttribute("aria-expanded")) === "false")
			await recordings.click();
		await pages[0].getByRole("link", { name: "History", exact: true }).click();
		await expect
			.poll(async () => (await stats()).feeds["video.activeDownloadsLive"])
			.toBe(2);
		await expect
			.poll(async () => (await stats()).feeds["storage.statusLive"])
			.toBe(3);
	} finally {
		await Promise.all(pages.map((page) => page.close()));
	}
	await expect
		.poll(
			async () =>
				(await (await request.get(`${origin}/test/stats`)).json()).connections,
		)
		.toBe(0);
});

test("continuous traffic stays on one connection beyond the former idle timeout", async ({
	page,
	request,
}) => {
	test.setTimeout(90_000);
	const stats = async () => (await request.get(`${origin}/test/stats`)).json();
	const before = await stats();
	await page.goto(`${origin}/dashboard`);
	await expect
		.poll(async () => (await stats()).samples, {
			timeout: 80_000,
			intervals: [1_000],
		})
		.toBeGreaterThan(before.samples + 72);
	const after = await stats();
	expect(after.opened - before.opened).toBe(1);
	expect(after.connections).toBe(1);
});
