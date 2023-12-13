import { t } from "i18next";
import { Quality } from "../type";
import i18n from "../i18n";

export function capitalizeFirstLetter(str: string): string {
    return str.charAt(0).toUpperCase() + str.slice(1).toLowerCase();
}

export function truncateString(str: string, num: number, etc = true): string {
    if (str.length <= num) {
        return str;
    }
    let newStr = str.slice(0, num);
    if (etc) {
        newStr += "...";
    }
    return newStr;
}

export const formatDate = (dateString: Date, timeZone: string, includeTime = true): string => {
    const options: Intl.DateTimeFormatOptions = {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        timeZone: timeZone,
    };

    if (includeTime) {
        options.hour = "2-digit";
        options.minute = "2-digit";
        options.second = "2-digit";
    }

    let date = new Intl.DateTimeFormat("en-GB", options).format(new Date(dateString));
    return date.replace(/\//g, "-").replace(",", "");
};

export const formatDuration = (duration?: number | null): string => {
    if (!duration) return "00:00:00";
    const flooredDuration = Math.floor(duration);
    const hours = Math.floor(flooredDuration / 3600)
        .toString()
        .padStart(2, "0");
    const minutes = Math.floor((flooredDuration % 3600) / 60)
        .toString()
        .padStart(2, "0");
    const seconds = (flooredDuration % 60).toString().padStart(2, "0");

    return `${hours}:${minutes}:${seconds}`;
};

export const toKebabCase = (str: string): string => {
    return str.toLowerCase().replace(/\s+/g, "-");
};

export const toTitleCase = (str?: string): string => {
    if (!str) return "";
    return str
        .split("-")
        .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
        .join(" ");
};

export const mapQuality = (value: string): Quality => {
    const mapping: { [key: string]: Quality } = {
        "480": Quality.LOW,
        "720": Quality.MEDIUM,
        "1080": Quality.HIGH,
    };
    return mapping[value] || Quality.MEDIUM; // default to MEDIUM
};

export function convertMillisecondsToTimeUnits(milliseconds: number) {
    const totalSeconds = Math.floor(milliseconds / 1000);
    const totalMinutes = Math.floor(totalSeconds / 60);
    const totalHours = Math.floor(totalMinutes / 60);
    const totalDays = Math.floor(totalHours / 24);
    return { totalSeconds, totalMinutes, totalHours, totalDays };
}

export function formatInterval(milliseconds: number) {
    const { totalSeconds, totalMinutes, totalHours, totalDays } = convertMillisecondsToTimeUnits(milliseconds);
    const days = totalDays;
    const hours = totalHours % 24;
    const minutes = totalMinutes % 60;
    const seconds = totalSeconds % 60;
    const millis = milliseconds % 1000;

    const result = [];

    if (days > 0) result.push(`${days} ${t("days")}${days > 1 ? "s" : ""}`);
    if (hours > 0) result.push(`${hours} ${t("hours")}${hours > 1 ? "s" : ""}`);
    if (minutes > 0) result.push(`${minutes} ${t("minutes")}${minutes > 1 ? "s" : ""}`);
    if (seconds > 0) result.push(`${seconds} ${t("seconds")}${seconds > 1 ? "s" : ""}`);
    if (millis > 0) result.push(`${millis} ${t("milliseconds")}`);

    return result.join(", ");
}

export function formatIntervalFuture(milliseconds: number) {
    const { totalSeconds, totalMinutes, totalHours, totalDays } = convertMillisecondsToTimeUnits(milliseconds);
    if (totalSeconds < 60) {
        return t("now");
    } else if (totalMinutes < 60) {
        return `${t("in")} ${totalMinutes} ${t("minutes")}`;
    } else if (totalHours < 24) {
        return `${t("in")} ${totalHours} ${t("hours")}`;
    } else if (totalDays === 1) {
        return t("in a day");
    } else if (totalDays > 1) {
        return t("in few days");
    }
    return t("in a while");
}

export function formatIntervalPast(milliseconds: number) {
    const { totalSeconds, totalMinutes, totalHours, totalDays } = convertMillisecondsToTimeUnits(milliseconds);
    if (totalSeconds < 60) {
        return t("a few seconds ago");
    } else if (totalMinutes === 1) {
        return t("a minute ago");
    } else if (totalMinutes < 60) {
        if (i18n.language === "fr") {
            return `Il y a ${totalMinutes} ${t("minutes")}`;
        }
        return `${totalMinutes} ${t("minutes ago")}`;
    } else if (totalHours === 1) {
        return t("an hour ago");
    } else if (totalHours < 24) {
        if (i18n.language === "fr") {
            return `Il y a ${totalHours} ${t("hours")}`;
        }
        return `${totalHours} ${t("hours ago")}`;
    } else if (totalDays === 1) {
        return t("a day ago");
    } else {
        if (i18n.language === "fr") {
            return `Il y a ${totalDays} ${t("days")}`;
        }
        return `${totalDays} ${t("days ago")}`;
    }
    return t("a while ago");
}
