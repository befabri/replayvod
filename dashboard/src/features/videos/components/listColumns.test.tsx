// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { TFunction } from "i18next";
import { afterEach, expect, it } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";
import { VideoThumbnail } from "./listColumns";

afterEach(cleanup);

it("replaces an unavailable poster and tries a new poster when the row changes", () => {
	const video = {
		display_name: "Recording",
		thumbnail: "thumbnails/old.jpg",
		duration_seconds: 60,
	} as VideoResponse;
	const t = ((key: string) => key) as TFunction;
	const { rerender } = render(<VideoThumbnail video={video} t={t} />);
	fireEvent.error(screen.getByRole("presentation"));
	expect(screen.queryByRole("presentation")).toBeNull();
	rerender(
		<VideoThumbnail
			video={{ ...video, thumbnail: "thumbnails/new.jpg" }}
			t={t}
		/>,
	);
	expect(screen.getByRole("presentation").getAttribute("src")).toContain(
		"thumbnails/new.jpg",
	);
});
