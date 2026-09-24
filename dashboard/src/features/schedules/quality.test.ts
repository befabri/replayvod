import { createInstance } from "i18next";
import { expect, it } from "vitest";
import en from "@/i18n/locales/en.json";
import fr from "@/i18n/locales/fr.json";
import { RECORDING_QUALITIES } from "@/lib/recording-settings";
import { scheduleQualityLabel } from "./quality";

it.each(["en", "fr"])("labels every quality in %s", async (lng) => {
	const i18n = createInstance();
	await i18n.init({
		lng,
		resources: { en: { translation: en }, fr: { translation: fr } },
	});
	for (const value of RECORDING_QUALITIES) {
		expect(i18n.exists(`schedules.quality_${value.toLowerCase()}`)).toBe(true);
		expect(scheduleQualityLabel(i18n.t, value)).not.toBe(value);
	}
	expect(scheduleQualityLabel(i18n.t, "future")).toBe("future");
});
