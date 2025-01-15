import path from "path";

export const SECRET_FILENAME = "secret";
export const LOG_FILENAME = "replayvod.log";

export const YOUTUBE_DL_DIR = "bin";
export const LOG_DIR = "logs";
const DATA_DIR = "data";
const VIDEO_DIR = "videos";
const THUMBNAIL_DIR = "thumbnail";
const SECRET_DIR = "secret";

export const ROOT_DIR = path.join(process.cwd());
export const PUBLIC_PATH = path.resolve(ROOT_DIR, DATA_DIR);
export const VIDEO_PATH = path.resolve(ROOT_DIR, DATA_DIR, VIDEO_DIR);
export const THUMBNAIL_PATH = path.resolve(ROOT_DIR, DATA_DIR, THUMBNAIL_DIR);
export const SECRET_PATH = path.resolve(ROOT_DIR, SECRET_DIR);
export const LOG_PATH = path.resolve(LOG_DIR, LOG_FILENAME);
