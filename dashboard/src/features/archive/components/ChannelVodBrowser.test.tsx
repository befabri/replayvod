// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
	ChannelVODsResponse,
	EnqueueArchiveResponse,
} from "@/api/generated/trpc";
import { DEFAULT_ARCHIVE_SETTINGS } from "@/features/archive/settings";

const state = vi.hoisted(() => ({
	channel: null as string | null,
	vods: {
		data: undefined as { pages: ChannelVODsResponse[] } | undefined,
		isFetching: false,
		isError: false,
		error: null as Error | null,
		hasNextPage: false,
		isFetchingNextPage: false,
		fetchNextPage: vi.fn(),
	},
	enqueue: {
		mutateAsync: vi.fn<(input: unknown) => Promise<EnqueueArchiveResponse>>(),
		isPending: false,
	},
	toast: { success: vi.fn(), info: vi.fn(), error: vi.fn() },
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string, vars?: Record<string, unknown>) =>
			typeof vars === "object" && vars !== null && "count" in vars
				? `${key}:${vars.count}`
				: typeof vars === "object" && vars !== null && "title" in vars
					? `${key}:${vars.title}`
					: key,
		i18n: { language: "en" },
	}),
}));
vi.mock("sonner", () => ({ toast: state.toast }));
vi.mock("@/features/archive/queries", () => ({
	useChannelVods: (channel: string | null) => {
		state.channel = channel;
		return state.vods;
	},
	useEnqueueArchive: () => state.enqueue,
}));
vi.mock("@/components/ui/avatar", () => ({
	Avatar: ({ name }: { name: string }) => createElement("span", null, name),
}));

import { ChannelVodBrowser } from "./ChannelVodBrowser";

function page(): ChannelVODsResponse {
	return {
		channel: { broadcaster_id: "u1", login: "streamer", name: "Streamer" },
		vods: [
			{
				id: "11",
				title: "Free one",
				url: "https://www.twitch.tv/videos/11",
				type: "archive",
				created_at: "2026-08-20T12:00:00Z",
				duration_seconds: 3723,
				view_count: 3,
			},
			{
				id: "12",
				title: "Held one",
				url: "https://www.twitch.tv/videos/12",
				type: "highlight",
				created_at: "2026-08-21T12:00:00Z",
				duration_seconds: 60,
				view_count: 1,
				archived_video_id: 5,
				archived_status: "RUNNING",
			},
		],
	};
}

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
	state.vods.data = undefined;
	state.vods.hasNextPage = false;
});

describe("ChannelVodBrowser", () => {
	it("only looks a channel up on submit with a usable name", () => {
		render(
			createElement(ChannelVodBrowser, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);
		const input = screen.getByLabelText("archive.channel_label");
		const lookup = screen.getByRole("button", {
			name: "archive.channel_lookup",
		});
		expect(lookup.hasAttribute("disabled")).toBe(true);
		fireEvent.change(input, {
			target: { value: "https://www.twitch.tv/videos/1" },
		});
		expect(lookup.hasAttribute("disabled")).toBe(true);
		expect(input.getAttribute("aria-invalid")).toBe("true");
		fireEvent.change(input, {
			target: { value: "https://www.twitch.tv/Streamer/videos" },
		});
		expect(state.channel).toBeNull();
		fireEvent.click(lookup);
		expect(state.channel).toBe("streamer");
	});

	it("lists VODs, disables selection of held ones, and queues the selected ids", async () => {
		state.vods.data = { pages: [page()] };
		state.enqueue.mutateAsync.mockResolvedValue({
			items: [{ input: "11", vod_id: "11", status: "queued" }],
		});
		render(
			createElement(ChannelVodBrowser, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);

		expect(screen.getByText("Free one")).toBeTruthy();
		expect(screen.getByText("archive.type_highlight")).toBeTruthy();
		expect(screen.getByText("1:02:03")).toBeTruthy();
		// The held VOD shows its library status and has no checkbox.
		expect(screen.getByText("videos.status.RUNNING")).toBeTruthy();
		expect(
			screen.queryByRole("checkbox", { name: "archive.select_vod:Held one" }),
		).toBeNull();

		const archive = screen.getByRole("button", {
			name: "archive.archive_selected",
		});
		expect(archive.hasAttribute("disabled")).toBe(true);
		fireEvent.click(
			screen.getByRole("checkbox", { name: "archive.select_all" }),
		);
		const selected = await screen.findByRole("button", {
			name: "archive.archive_selected_count:1",
		});
		fireEvent.click(selected);
		await waitFor(() =>
			expect(state.enqueue.mutateAsync).toHaveBeenCalledTimes(1),
		);
		expect(state.enqueue.mutateAsync).toHaveBeenCalledWith({
			vods: ["11"],
			recording_type: "video",
			quality: "HIGH",
			force_h264: false,
		});
		await waitFor(() =>
			expect(state.toast.success).toHaveBeenCalledWith(
				"archive.queued_toast:1",
			),
		);
	});

	// Selection is keyed by VOD id, not row position, so appending a page keeps
	// what was already ticked and queues that id rather than a neighbour's.
	it("keeps a picked VOD selected when another page arrives", async () => {
		state.vods.data = { pages: [page()] };
		state.enqueue.mutateAsync.mockResolvedValue({ items: [] });
		const { rerender } = render(
			createElement(ChannelVodBrowser, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);

		fireEvent.click(
			screen.getByRole("checkbox", { name: "archive.select_vod:Free one" }),
		);
		await screen.findByRole("button", {
			name: "archive.archive_selected_count:1",
		});

		const next = page();
		next.vods = [
			{
				id: "13",
				title: "Newer one",
				url: "https://www.twitch.tv/videos/13",
				type: "archive",
				created_at: "2026-08-22T12:00:00Z",
				duration_seconds: 90,
				view_count: 2,
			},
		];
		state.vods.data = { pages: [page(), next] };
		rerender(
			createElement(ChannelVodBrowser, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);

		expect(screen.getByText("Newer one")).toBeTruthy();
		fireEvent.click(
			await screen.findByRole("button", {
				name: "archive.archive_selected_count:1",
			}),
		);
		await waitFor(() =>
			expect(state.enqueue.mutateAsync).toHaveBeenCalledWith(
				expect.objectContaining({ vods: ["11"] }),
			),
		);
	});

	it("splits a selection larger than one call into batches in list order", async () => {
		const first = page();
		first.vods = Array.from({ length: 26 }, (_, i) => ({
			id: String(100 + i),
			title: `vod ${100 + i}`,
			url: `https://www.twitch.tv/videos/${100 + i}`,
			type: "archive",
			created_at: "2026-08-20T12:00:00Z",
			duration_seconds: 60,
			view_count: 1,
		}));
		const second = page();
		second.vods = Array.from({ length: 25 }, (_, i) => ({
			id: String(200 + i),
			title: `vod ${200 + i}`,
			url: `https://www.twitch.tv/videos/${200 + i}`,
			type: "archive",
			created_at: "2026-08-19T12:00:00Z",
			duration_seconds: 60,
			view_count: 1,
		}));
		state.vods.data = { pages: [first, second] };
		state.enqueue.mutateAsync.mockImplementation(async (input) => ({
			items: (input as { vods: string[] }).vods.map((id) => ({
				input: id,
				vod_id: id,
				status: "queued" as const,
			})),
		}));
		render(
			createElement(ChannelVodBrowser, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);
		fireEvent.click(
			screen.getByRole("checkbox", { name: "archive.select_all" }),
		);
		fireEvent.click(
			await screen.findByRole("button", {
				name: "archive.archive_selected_count:51",
			}),
		);
		await waitFor(() =>
			expect(state.enqueue.mutateAsync).toHaveBeenCalledTimes(2),
		);
		const calls = state.enqueue.mutateAsync.mock.calls.map(
			([input]) => (input as { vods: string[] }).vods,
		);
		expect(calls[0]).toHaveLength(50);
		expect(calls[0]?.[0]).toBe("100");
		expect(calls[1]).toEqual(["224"]);
		await waitFor(() =>
			expect(state.toast.success).toHaveBeenCalledWith(
				"archive.queued_toast:51",
			),
		);
		expect(state.toast.success).toHaveBeenCalledTimes(1);
	});

	it("offers no checkbox for live, private, or live-recorded VODs and labels them", () => {
		const p = page();
		p.vods = [
			{
				id: "21",
				title: "On air",
				url: "https://www.twitch.tv/videos/21",
				type: "archive",
				created_at: "2026-08-20T12:00:00Z",
				duration_seconds: 60,
				view_count: 1,
				live: true,
			},
			{
				id: "22",
				title: "Subs only",
				url: "https://www.twitch.tv/videos/22",
				type: "archive",
				created_at: "2026-08-20T12:00:00Z",
				duration_seconds: 60,
				view_count: 1,
				viewable: "private",
			},
			{
				id: "23",
				title: "Recorded whole",
				url: "https://www.twitch.tv/videos/23",
				type: "archive",
				created_at: "2026-08-20T12:00:00Z",
				duration_seconds: 60,
				view_count: 1,
				archived_video_id: 40,
				archived_status: "DONE",
				held_reason: "live_recording",
			},
		];
		state.vods.data = { pages: [p] };
		render(
			createElement(ChannelVodBrowser, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);
		expect(screen.getByText("archive.live_now")).toBeTruthy();
		expect(screen.getByText("archive.private")).toBeTruthy();
		expect(screen.getByText("archive.recorded_live")).toBeTruthy();
		expect(screen.getByText("videos.status.DONE")).toBeTruthy();
		for (const title of ["On air", "Subs only", "Recorded whole"]) {
			expect(
				screen.queryByRole("checkbox", { name: `archive.select_vod:${title}` }),
			).toBeNull();
		}
		// Select-all has nothing to pick, so the action stays inert.
		fireEvent.click(
			screen.getByRole("checkbox", { name: "archive.select_all" }),
		);
		expect(
			screen
				.getByRole("button", { name: "archive.archive_selected" })
				.hasAttribute("disabled"),
		).toBe(true);
	});

	it("shows a load-more control when Twitch has another page", () => {
		state.vods.data = { pages: [page()] };
		state.vods.hasNextPage = true;
		render(
			createElement(ChannelVodBrowser, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);
		fireEvent.click(screen.getByRole("button", { name: "archive.load_more" }));
		expect(state.vods.fetchNextPage).toHaveBeenCalled();
	});
});
