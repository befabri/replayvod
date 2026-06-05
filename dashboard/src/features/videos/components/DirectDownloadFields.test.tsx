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

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));

import { useDirectDownloadForm } from "../download-form";
import { DirectDownloadFields } from "./DirectDownloadFields";

function Harness({ disabled }: { disabled?: boolean }) {
	const form = useDirectDownloadForm(async () => {});
	return createElement(DirectDownloadFields, { form, disabled });
}

// The quality Select trigger (a button) and the Force H.264 checkbox both carry
// the native `disabled` attribute exactly when they're inert. The shared field
// component namespaces ids via useId, so match on the stable suffix to assert
// the real control state without depending on Base UI's node structure.
function disabledState(container: HTMLElement) {
	return {
		quality: container
			.querySelector('[id$="-quality"]')
			?.hasAttribute("disabled"),
		forceH264: container
			.querySelector('[id$="-force-h264"]')
			?.hasAttribute("disabled"),
	};
}

afterEach(cleanup);

describe("DirectDownloadFields", () => {
	it("leaves quality and Force H.264 enabled in video mode", () => {
		const { container } = render(createElement(Harness, {}));
		expect(disabledState(container)).toEqual({
			quality: false,
			forceH264: false,
		});
	});

	it("disables quality and Force H.264 when audio is selected", async () => {
		const { container } = render(createElement(Harness, {}));
		fireEvent.click(screen.getByRole("radio", { name: "videos.mode_audio" }));
		await waitFor(() => {
			expect(disabledState(container)).toEqual({
				quality: true,
				forceH264: true,
			});
		});
	});

	it("clears Force H.264 when audio is selected", async () => {
		render(createElement(Harness, {}));
		const forceH264 = screen.getByRole("checkbox", {
			name: "videos.force_h264",
		});

		fireEvent.click(forceH264);
		await waitFor(() => {
			expect(forceH264.getAttribute("aria-checked")).toBe("true");
		});

		fireEvent.click(screen.getByRole("radio", { name: "videos.mode_audio" }));
		await waitFor(() => {
			expect(forceH264.getAttribute("aria-checked")).toBe("false");
		});
	});

	it("disables the video-only controls when the field is disabled (offline)", () => {
		const { container } = render(createElement(Harness, { disabled: true }));
		expect(disabledState(container)).toEqual({
			quality: true,
			forceH264: true,
		});
	});
});
