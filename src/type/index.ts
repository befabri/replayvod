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
  isChecked?: boolean;
}

export interface TableProps {
  items: Video[];
}
