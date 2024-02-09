import path from "path";
import { env } from "../app";

export const YOUTUBE_DL_DIR = "bin";
export const LOG_DIR = "logs";
export const DATA_DIR = "data";
export const ROOT_DIR = env.nodeEnv === "production" ? path.resolve(__dirname) : path.resolve(__dirname, "..");
const PUBLIC_DIR = "public";
const VIDEO_DIR = "videos";
const THUMBNAIL_DIR = "thumbnail";
export const PUBLIC_PATH = path.resolve(ROOT_DIR, PUBLIC_DIR);
export const VIDEO_PATH = path.resolve(ROOT_DIR, PUBLIC_DIR, VIDEO_DIR);
export const THUMBNAIL_PATH = path.resolve(ROOT_DIR, PUBLIC_DIR, THUMBNAIL_DIR);
