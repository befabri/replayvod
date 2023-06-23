import { Document, ObjectId } from "mongodb";

export interface Log {
  _id?: ObjectId;
  id: number;
  filename: string;
  downloadUrl: string;
  lastWriteTime: Date;
  type: "YoutubeDl" | "FixVideos" | "Request" | "Error" | "Combined" | "Info" | "Connection";
}
