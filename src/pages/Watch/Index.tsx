import React, { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../../type";
import VideoInfoComponent from "../../components/Media/VideoInfo";
import { ApiRoutes, getApiRoute } from "../../type/routes";
import NotFound from "../../components/Others/NotFound";

const WatchPage: React.FC = () => {
    const { t } = useTranslation();
    const { id } = useParams();

    const [isLoading, setIsLoading] = useState<boolean>(true);
    const [video, setVideo] = useState<CompletedVideo>(null);
    const [playVideo, setPlayVideo] = useState("");
    const ROOT_URL = import.meta.env.VITE_ROOTURL;

    useEffect(() => {
        const fetchData = async () => {
            try {
                let url = getApiRoute(ApiRoutes.GET_VIDEO_ID, "id", id);
                const videoResponse = await fetch(url, { credentials: "include" });
                if (!videoResponse.ok) {
                    throw new Error(`HTTP error! status: ${videoResponse.status}`);
                }
                const videoData = await videoResponse.json();
                url = getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", videoData.thumbnail);
                const thumbnailResponse = await fetch(url, { credentials: "include" });
                if (!thumbnailResponse.ok) {
                    throw new Error(`HTTP error! status: ${thumbnailResponse.status}`);
                }
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
            {video ? (
                <div className="flex h-screen flex-col items-center">
                    <video
                        controls
                        controlsList="nodownload"
                        poster={video?.thumbnail}
                        preload="auto"
                        className="mx-auto mt-14 block max-h-screen w-full flex-grow overflow-auto bg-black object-contain">
                        <source src={playVideo} type="video/mp4" />
                        {t("Your browser does not support the video tag.")}
                    </video>

                    <div className="mb-4 ml-4 self-start">
                        <VideoInfoComponent video={video} />
                    </div>
                </div>
            ) : (
                <div className="p-4">
                    <div className="mt-14 p-4">
                        <NotFound text={t("No video found.")} />
                    </div>
                </div>
            )}
        </div>
    );
};

export default WatchPage;
