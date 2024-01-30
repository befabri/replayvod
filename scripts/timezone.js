import * as fs from "fs";
import * as path from "path";
import moment from "moment-timezone";

const timeZones = moment.tz.names();

const directoryPath = path.join("src/assets");
const filePath = path.join(directoryPath, "timezones.json");

// Create the directory if it doesn't exist
if (!fs.existsSync(directoryPath)) {
    fs.mkdirSync(directoryPath, { recursive: true });
}

// Write the time zones to the file
fs.writeFile(filePath, JSON.stringify(timeZones, null, 2), (err) => {
    if (err) {
        console.error("Error writing file:", err);
        return;
    }
    console.log("Time zones saved to", filePath);
});
