// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WatchLaterButton } from "./WatchLaterButton";

const mutateMock = vi.hoisted(() => vi.fn());

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string) => key,
	}),
}));

vi.mock("@/features/videos", () => ({
	useSetWatchLater: () => ({
		isPending: false,
		mutate: mutateMock,
	}),
}));

afterEach(() => {
	cleanup();
	mutateMock.mockReset();
});

describe("WatchLaterButton", () => {
	it("adds a video to watch later", () => {
		render(<WatchLaterButton videoId={42} watchLater={false} />);

		fireEvent.click(screen.getByRole("button"));

		expect(mutateMock).toHaveBeenCalledWith(
			{ video_id: 42, watch_later: true },
			expect.objectContaining({
				onError: expect.any(Function),
				onSuccess: expect.any(Function),
			}),
		);
	});

	it("removes a video from watch later", () => {
		render(<WatchLaterButton videoId={42} watchLater />);

		fireEvent.click(screen.getByRole("button"));

		expect(mutateMock).toHaveBeenCalledWith(
			{ video_id: 42, watch_later: false },
			expect.any(Object),
		);
	});
});
