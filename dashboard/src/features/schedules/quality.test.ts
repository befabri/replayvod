import { createInstance } from "i18next";
import { expect, it } from "vitest";
import en from "@/i18n/locales/en.json";
import fr from "@/i18n/locales/fr.json";
import { scheduleQualityLabel, scheduleQualityOptions } from "./quality";

it.each(["en", "fr"])("labels every quality in %s", async (lng) => {
	const i18n = createInstance();
	await i18n.init({
		lng,
		resources: { en: { translation: en }, fr: { translation: fr } },
	});
	const options = scheduleQualityOptions(i18n.t);
	expect(options.map((option) => option.value)).toEqual([
		"BEST",
		"1440",
		"HIGH",
		"MEDIUM",
		"LOW",
	]);
	for (const { value, label } of options) {
		expect(i18n.exists(`schedules.quality_${value.toLowerCase()}`)).toBe(true);
		expect(label).toBe(scheduleQualityLabel(i18n.t, value));
		expect(label).not.toBe(value);
	}
	expect(scheduleQualityLabel(i18n.t, "future")).toBe("future");
});
