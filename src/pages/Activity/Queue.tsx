import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Table from "../../components/Tables";

interface Video {
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
  game_id: string[];
  title: string[];
  tags: string[];
  viewer_count: number[];
  language: string;
  isChecked?: boolean;
}

const Queue: React.FC = () => {
  const { t } = useTranslation();
  const [videos, setVideos] = useState<Video[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const fetchData = async () => {
      const response = await fetch("http://localhost:3000/api/videos/all", {
        credentials: "include",
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      const data = await response.json();

      const pendingVideos = data.filter((video: Video) => video.status === "Pending");

      const convertedVideos = pendingVideos.map((video: Video) => {
        const startDownloadAtDate = new Date(video.start_download_at);
        const downloadedAtDate = new Date(video.downloaded_at);
        video.start_download_at = startDownloadAtDate.toLocaleString("fr-FR", { timeZone: "Europe/Paris" });
        video.downloaded_at = downloadedAtDate.toLocaleString("fr-FR", { timeZone: "Europe/Paris" });
        return video;
      });

      setVideos((prevVideos) => {
        const updatedVideos = prevVideos.map((video) => {
          const newVideo = convertedVideos.find((v: { id: string }) => v.id === video.id);
          return newVideo || video;
        });
        return [
          ...updatedVideos,
          ...convertedVideos.filter((v: { id: string }) => !prevVideos.find((video) => v.id === video.id)),
        ];
      });
      setIsLoading(false);
    };

    fetchData();
    const intervalId = setInterval(fetchData, 10000);

    return () => clearInterval(intervalId);
  }, []);

  return (
    <div className="p-4 sm:ml-64">
      <div className="p-4 mt-14">
        <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Queue")}</h1>
      </div>
      {isLoading ? <div>Loading...</div> : <Table items={videos} />}
    </div>
  );
};

export default Queue;
