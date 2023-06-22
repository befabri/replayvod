import React, { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";

const Vod: React.FC = () => {
  const { t } = useTranslation();
  // const [videoSrc, setVideoSrc] = useState<string | null>(null);
  const [videos, setVideos] = useState<any[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const ROOT_URL = import.meta.env.VITE_ROOTURL;

  const fetchImage = async (url) => {
    const response = await fetch(url, { credentials: "include" });
    const blob = await response.blob();
    return URL.createObjectURL(blob);
  };

  useEffect(() => {
    fetch(`${ROOT_URL}/api/videos/finished`, {
      credentials: "include",
    })
      .then((response) => {
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.json();
      })
      .then(async (data) => {
        const promises = data.map(async (video) => {
          const imageUrl = await fetchImage(`${ROOT_URL}/api/videos/thumbnail/${video.thumbnail}`);
          return { ...video, thumbnail: imageUrl };
        });

        const videosWithBlobUrls = await Promise.all(promises);
        setVideos(videosWithBlobUrls);
        setIsLoading(false);
      })
      .catch((error) => {
        console.error(`Error fetching data: ${error}`);
      });
  }, []);

  // const playVideo = (videoId: string) => {
  //   setVideoSrc(`${ROOT_URL}/api/videos/play/${videoId}`);
  // };

  if (isLoading) {
    return <p>{t("Loading")}</p>;
  }

  return (
    <div className="p-4 sm:ml-64">
      <div className="p-4 mt-14">
        <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Videos")}</h1>
      </div>
      {videos.map((video) => (
        <div key={video.id} className="mb-4">
          <h2 className="text-1xl  dark:text-stone-100">{video.display_name}</h2>
          <p className="text-medium  dark:text-stone-100">
            {t("Size")}: {video.size}
          </p>
          <p className="text-medium  dark:text-stone-100">
            {t("Downloaded at")}: {video.downloaded_at}
          </p>
          <video controls poster={video.thumbnail} preload="none" className="w-full max-w-lg">
            <source src={`${ROOT_URL}/api/videos/play/${video.id}`} type="video/mp4" />
            {t("Your browser does not support the video tag.")}
          </video>
        </div>
      ))}

      {/* {videoSrc && (
        <div>
          <video controls className="w-full max-w-lg">
            <source src={videoSrc} type="video/mp4" />
            {t("Your browser does not support the video tag.")}
          </video>
        </div>
      )} */}
    </div>
  );
};

export default Vod;
