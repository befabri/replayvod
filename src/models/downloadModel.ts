import { Document, ObjectId } from "mongodb";

export interface DownloadSchedule {
  _id?: ObjectId;
  source: string;
  channelName: string;
  viewersCount: number;
  timeBeforeDelete: number;
  trigger: string;
  tag: string;
  category: string;
  quality: string;
  isDeleteRediff: boolean;
  requested_by: string;
}

export enum VideoQuality {
  LOW = "480p",
  MEDIUM = "720p",
  HIGH = "1080p",
}
