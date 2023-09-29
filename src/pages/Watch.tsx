import React, { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../type";
import VideoInfoComponent from "../components/VideoInfo";

const Watch: React.FC = () => {
    const { t } = useTranslation();
    const { id } = useParams();

    const [isLoading, setIsLoading] = useState<boolean>(true);
    const [video, setVideo] = useState<CompletedVideo>();
    const ROOT_URL = import.meta.env.VITE_ROOTURL;

    useEffect(() => {
        const fetchData = async () => {
            try {
                const videoResponse = await fetch(`${ROOT_URL}/api/videos/${id}`, { credentials: "include" });
                const videoData = await videoResponse.json();

                const thumbnailBlob = await fetch(`${ROOT_URL}/api/videos/thumbnail/${videoData.thumbnail}`, {
                    credentials: "include",
                }).then((res) => res.blob());

                setVideo({
                    ...videoData,
                    thumbnail: URL.createObjectURL(thumbnailBlob),
                });
                setIsLoading(false);
            } catch (error) {
                console.error("Error:", error);
                setIsLoading(false);
            }
        };

        fetchData();
    }, [id, ROOT_URL]);

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    return (
        <div>
            <div className="flex flex-col items-center h-screen">
                <video
                    controls
                    controlsList="nodownload"
                    poster={video?.thumbnail}
                    preload="auto"
                    className="mt-14 flex-grow overflow-auto w-full max-h-screen block mx-auto bg-black object-contain">
                    <source src={`${ROOT_URL}/api/videos/play/${video?.id}`} type="video/mp4" />
                    {t("Your browser does not support the video tag.")}
                </video>

                <div className="self-start w-full mb-4">
                    <VideoInfoComponent video={video} />
                </div>
            </div>
        </div>
    );
};

export default Watch;
