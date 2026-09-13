// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";

const usePagesMock = vi.hoisted(() => vi.fn());
vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/features/videos/queries", () => ({
	useInfiniteVideoPages: usePagesMock,
}));
vi.mock("@/features/videos/permissions", () => ({
	useCanManageVideos: () => false,
}));
vi.mock("@/components/ui/view-all-link", () => ({
	ViewAllLink: () => <a href="/dashboard/videos?status=DONE">View all</a>,
}));
vi.mock("@/features/videos/components/VideoCard", () => ({
	VideoCard: ({ video }: { video: VideoResponse }) => (
		<div data-testid="card">{video.id}</div>
	),
}));

import { LatestRecordings } from "./LatestRecordings";

afterEach(() => {
	cleanup();
	usePagesMock.mockReset();
});

describe("LatestRecordings", () => {
	it("shows loading and hides only after a successful empty response", () => {
		usePagesMock.mockReturnValue({ data: undefined, isLoading: true });
		const { rerender } = render(<LatestRecordings />);
		expect(screen.getByRole("status", { name: "common.loading" })).toBeTruthy();
		usePagesMock.mockReturnValue({ data: { pages: [{ items: [] }] } });
		rerender(<LatestRecordings />);
		expect(screen.queryByTestId("latest-recordings")).toBeNull();
	});

	it("shows request errors and lets the viewer retry", () => {
		const refetch = vi.fn();
		usePagesMock.mockReturnValue({
			error: new Error("unavailable"),
			refetch,
			isFetching: false,
		});
		render(<LatestRecordings />);
		expect(screen.getByRole("alert").textContent).toContain(
			"videos.failed_to_load",
		);
		fireEvent.click(screen.getByRole("button", { name: "common.retry" }));
		expect(refetch).toHaveBeenCalledOnce();
	});

	it("keeps cached recordings visible when a refresh fails", () => {
		usePagesMock.mockReturnValue({
			data: { pages: [{ items: [{ id: 1, status: "DONE" }] }] },
			error: new Error("unavailable"),
			refetch: vi.fn(),
			isFetching: true,
		});
		render(<LatestRecordings />);
		expect(screen.getByTestId("card").textContent).toBe("1");
		expect(screen.getByRole("alert")).toBeTruthy();
		expect(
			(
				screen.getByRole("button", {
					name: "common.retry",
				}) as HTMLButtonElement
			).disabled,
		).toBe(true);
	});

	it("requests the newest completed recordings and caps the preview at five", () => {
		usePagesMock.mockReturnValue({
			data: {
				pages: [
					{
						items: [
							{ id: 9, status: "RUNNING" },
							{ id: 8, status: "PENDING" },
							{ id: 7, status: "FAILED" },
							{ id: 6, status: "DONE", deleted_at: "2026-09-13T00:00:00Z" },
							...[5, 4, 3, 2, 1, 0].map((id) => ({ id, status: "DONE" })),
						],
					},
				],
			},
		});
		render(<LatestRecordings />);
		expect(usePagesMock).toHaveBeenCalledWith(5, "DONE", "created_at", "desc");
		expect(
			screen.getAllByTestId("card").map((card) => card.textContent),
		).toEqual(["5", "4", "3", "2", "1"]);
	});
});
