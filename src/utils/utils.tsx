export function capitalizeFirstLetter(str: string): string {
    return str.charAt(0).toUpperCase() + str.slice(1).toLowerCase();
}

export function truncateString(str: string, num: number, etc: boolean = true): string {
    if (str.length <= num) {
        return str;
    }
    let newStr = str.slice(0, num);
    if (etc) {
        newStr += "...";
    }
    return newStr;
}

export const formatDate = (dateString: Date, timeZone: string, includeTime: boolean = true): string => {
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
