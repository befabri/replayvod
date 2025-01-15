import { DateTime } from "luxon";
import { TIMEZONE } from "../constants/constant.server";

export function chunkArray<T>(array: T[], chunkSize: number): T[][] {
    let index = 0;
    let arrayLength = array.length;
    let tempArray = [];

    for (index = 0; index < arrayLength; index += chunkSize) {
        let myChunk = array.slice(index, index + chunkSize);
        tempArray.push(myChunk);
    }

    return tempArray;
}

export function delay(milliseconds: number) {
    return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

export const formatTimestamp = () => {
    const formattedDate = DateTime.now().setZone(TIMEZONE).toFormat("yyyy-MM-dd HH:mm:ss");
    return `,"time":"${formattedDate}"`;
};
