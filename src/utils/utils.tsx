export function capitalizeFirstLetter(str: string): string {
    return str.charAt(0).toUpperCase() + str.slice(1).toLowerCase();
}

export function truncateString(str: string, num: number): string {
    if (str.length <= num) {
        return str;
    }
    return str.slice(0, num) + "...";
}

export const formatDate = (dateString: Date, timeZone: string): string => {
    const options: Intl.DateTimeFormatOptions = {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        timeZone: timeZone,
    };
    let date = new Intl.DateTimeFormat("en-GB", options).format(new Date(dateString));
    return date.replace(/\//g, "-").replace(",", "");
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
