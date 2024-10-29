import path from "path";

export const YOUTUBE_DL_DIR = "bin";
export const LOG_DIR = "logs";
export const ROOT_DIR = path.resolve(__dirname, "..");
const DATA_DIR = "data";
const VIDEO_DIR = "videos";
const THUMBNAIL_DIR = "thumbnail";
const SECRET_DIR = "secret";
export const PUBLIC_PATH = path.resolve(ROOT_DIR, DATA_DIR);
export const VIDEO_PATH = path.resolve(ROOT_DIR, DATA_DIR, VIDEO_DIR);
export const THUMBNAIL_PATH = path.resolve(ROOT_DIR, DATA_DIR, THUMBNAIL_DIR);
export const SECRET_PATH = path.resolve(ROOT_DIR, SECRET_DIR);
