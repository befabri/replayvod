import React, { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../../type";
import VideoInfoComponent from "../../components/Media/VideoInfo";
import { ApiRoutes, getApiRoute } from "../../type/routes";

const WatchPage: React.FC = () => {
    const { t } = useTranslation();
    const { id } = useParams();

    const [isLoading, setIsLoading] = useState<boolean>(true);
    const [video, setVideo] = useState<CompletedVideo>();
    const [playVideo, setPlayVideo] = useState("");
    const ROOT_URL = import.meta.env.VITE_ROOTURL;

    useEffect(() => {
        const fetchData = async () => {
            try {
                let url = getApiRoute(ApiRoutes.GET_VIDEO_ID, "id", id);
                const videoResponse = await fetch(url, { credentials: "include" });
                const videoData = await videoResponse.json();

                url = getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", videoData.thumbnail);
                const thumbnailResponse = await fetch(url, { credentials: "include" });
                const thumbnailBlob = await thumbnailResponse.blob();

                setVideo({
                    ...videoData,
                    thumbnail: URL.createObjectURL(thumbnailBlob),
                });

                url = getApiRoute(ApiRoutes.GET_VIDEO_PLAY_ID, "id", videoData.id);
                setPlayVideo(url);
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
                    <source src={playVideo} type="video/mp4" />
                    {t("Your browser does not support the video tag.")}
                </video>

                <div className="self-start w-full mb-4">
                    <VideoInfoComponent video={video} />
                </div>
            </div>
        </div>
    );
};

export default WatchPage;
