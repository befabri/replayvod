import React, { useState, useEffect } from "react";

const Vod: React.FC = () => {
  const [videoSrc, setVideoSrc] = useState<string | null>(null);
  const [videos, setVideos] = useState<any[]>([]);
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
        console.log(videos);
        setIsLoading(false);
      })
      .catch((error) => {
        console.error(`Error fetching data: ${error}`);
      });
  }, []);

  const playVideo = (videoId: string) => {
    setVideoSrc(`http://localhost:3000/api/videos/play/${videoId}`);
  };

  if (isLoading) {
    return <p>Loading videos...</p>;
  }

  return (
    <div className="p-4 sm:ml-64">
      <div className="p-4 mt-14">
        <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">Videos</h1>
      </div>
      {videos.map((video) => (
        <div key={video.id} className="mb-4">
          <h2 className="text-1xl  dark:text-stone-100">{video.display_name}</h2>
          <p className="text-medium  dark:text-stone-100">Taille: {video.size}</p>
          <p className="text-medium  dark:text-stone-100">Téléchargé le: {video.downloaded_at}</p>
          <video controls poster={video.thumbnail} className="w-full max-w-lg">
            <source src={`http://localhost:3000/api/videos/play/${video._id}`} type="video/mp4" />
            Your browser does not support the video tag.
          </video>
        </div>
      ))}

      {videoSrc && (
        <div>
          <video controls className="w-full max-w-lg">
            <source src={videoSrc} type="video/mp4" />
            Your browser does not support the video tag.
          </video>
        </div>
      )}
    </div>
  );
};

export default Vod;
