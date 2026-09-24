import addonA11y from "@storybook/addon-a11y";
import addonDocs from "@storybook/addon-docs";
import { definePreview } from "@storybook/react-vite";
import { configure } from "storybook/test";
import "@/styles.css";
import { startClockAt } from "@/test/clock";
import { FIXTURE_NOW } from "@/test/fixtures";
import { acceptedPaletteContrast } from "./contrast";
import { withProviders } from "./providers";
import { theme } from "./theme";

const STORY_WAIT_TIMEOUT_MS = 5000;

export default definePreview({
	addons: [addonDocs(), addonA11y()],
	tags: ["autodocs"],
	globalTypes: {
		theme: {
			description: "Color theme",
			toolbar: {
				title: "Theme",
				icon: "mirror",
				items: [
					{ value: "dark", title: "Dark", icon: "moon" },
					{ value: "light", title: "Light", icon: "sun" },
				],
				dynamicTitle: true,
			},
		},
		locale: {
			description: "Interface language",
			toolbar: {
				title: "Locale",
				icon: "globe",
				items: [
					{ value: "en", right: "EN", title: "English" },
					{ value: "fr", right: "FR", title: "Français" },
				],
				dynamicTitle: true,
			},
		},
		role: {
			description: "Signed-in user role",
			toolbar: {
				title: "Role",
				icon: "user",
				items: [
					{ value: "owner", title: "Owner" },
					{ value: "admin", title: "Admin" },
					{ value: "viewer", title: "Viewer" },
					{ value: "signed-out", title: "Signed out" },
				],
				dynamicTitle: true,
			},
		},
	},
	initialGlobals: { theme: "dark", locale: "en", role: "owner" },
	parameters: {
		backgrounds: { disable: true },
		viewport: {
			options: {
				desktop: {
					name: "Desktop",
					styles: { width: "1440px", height: "900px" },
					type: "desktop",
				},
			},
		},
		docs: { theme },
		controls: {
			matchers: { color: /(background|color)$/i, date: /Date$/i },
		},
		a11y: {
			test: "error",
			config: { checks: [acceptedPaletteContrast] },
			context: { exclude: ["[data-base-ui-focus-guard]"] },
		},
	},
	decorators: [withProviders],
	beforeAll: () => {
		configure({ asyncUtilTimeout: STORY_WAIT_TIMEOUT_MS });
	},
	beforeEach: () => startClockAt(FIXTURE_NOW),
});
