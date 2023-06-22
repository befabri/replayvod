import { Document, ObjectId } from "mongodb";
import { VideoQuality } from "./downloadModel";

export interface Video {
  _id?: ObjectId;
  id: string;
  filename: string;
  status: string;
  display_name: string;
  broadcaster_id: string;
  requested_by: string;
  start_download_at: Date;
  downloaded_at: string;
  job_id: string;
  category: { id: string; name: string }[];
  title: string[];
  tags: string[];
  viewer_count: number;
  language: string;
  quality: VideoQuality;
  size?: number;
  thumbnail?: string;
}
