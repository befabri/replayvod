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

const Manage: React.FC = () => {
  const { t } = useTranslation();
  const [videos, setVideos] = useState<Video[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    fetch("http://localhost:3000/api/videos/all", {
      credentials: "include",
    })
      .then((response) => {
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.json();
      })
      .then((data) => {
        setVideos(data);
        console.log(data);
        setIsLoading(false);
      })
      .catch((error) => {
        console.error(`Error fetching data: ${error}`);
      });
  }, []);

  return (
    <div className="p-4 sm:ml-64">
      <div className="p-4 mt-14">
        <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Manage")}</h1>
      </div>
      {isLoading ? <div>Loading...</div> : <Table items={videos} />}
    </div>
  );
};

export default Manage;
