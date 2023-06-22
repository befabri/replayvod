export interface Video {
  _id?: string;
  id: string;
  filename: string;
  status: string;
  display_name: string;
  broadcaster_id: string;
  requested_by: string;
  start_download_at: string;
  downloaded_at: string;
  job_id: string;
  category: { id: string; name: string }[];
  title: string[];
  tags: string[];
  viewer_count: number[];
  language: string;
  size?: number;
  thumbnail?: string;
  isChecked?: boolean;
}

export interface TableProps {
  items: Video[];
}

export interface Task {
  _id: string;
  id: string;
  name: string;
  description: string;
  taskType: string;
  metadata?: {
    [key: string]: string;
  };
  interval: number;
  lastExecution: Date;
  lastDuration: number;
  nextExecution: Date;
}
