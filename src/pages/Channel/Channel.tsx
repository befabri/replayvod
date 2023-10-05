import React, { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../../type";
import VideoComponent from "../../components/Media/Video";
import { ApiRoutes, Pathnames, getApiRoute } from "../../type/routes";
import Button from "../../components/UI/Button/Button";

const Channel: React.FC = () => {
    const { t } = useTranslation();
    let { id } = useParams();

    const [isLoading, setIsLoading] = useState<boolean>(true);
    const [isFetching, setIsFetching] = useState<boolean>(false);
    // const [buttonText, setButtonText] = useState<string>("Enregistrer");
    const [videos, setVideos] = useState<CompletedVideo[]>([]);
    const [hasVideos, setHasVideos] = useState<boolean>(true);

    const handleClick = async () => {
        if (!isFetching) {
            if (!videos[0].channel.broadcasterId) {
                return;
            }
            setIsFetching(true);
            // setButtonText("En cours");
            let url = getApiRoute(ApiRoutes.GET_DOWNLOAD_STREAM_ID, "id", videos[0].channel.broadcasterId);
            try {
                const response = await fetch(url, { credentials: "include" });
                await response.json();
                // setButtonText("est enregistrer");
            } catch (error) {
                console.error("Error:", error);
            } finally {
                setIsFetching(false);
            }
        }
    };

    useEffect(() => {
        const fetchData = async () => {
            try {
                let url = getApiRoute(ApiRoutes.GET_VIDEO_CHANNEL_BROADCASTERLOGIN, "broadcasterLogin", id);
                const videoResponse = await fetch(url, { credentials: "include" });
                const videosData = await videoResponse.json();

                if (!videoResponse.ok) {
                    throw new Error(`HTTP error: ${videoResponse.status} ${videoResponse.statusText}`);
                }

                if (!videosData.hasVideos) {
                    setHasVideos(false);
                    setVideos([videosData]);
                    setIsLoading(false);
                    return;
                }
                const thumbnails = await Promise.all(
                    videosData.videos.map((video: CompletedVideo) => {
                        url = getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", video.thumbnail);
                        return fetch(url, {
                            credentials: "include",
                        }).then((res) => res.blob());
                    })
                );

                const updatedVideos = videosData.videos.map((video: CompletedVideo, index: number) => ({
                    ...video,
                    thumbnail: URL.createObjectURL(thumbnails[index]),
                }));

                setVideos(updatedVideos);
                setIsLoading(false);
            } catch (error) {
                console.error("Error:", error);
                setIsLoading(false);
            }
        };

        fetchData();
    }, []);

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    return (
        <div className="p-4">
            <div className="mt-14 flex flex-row items-center gap-3">
                <a href={`${Pathnames.Channel}${videos[0]?.channel.displayName.toLowerCase()}`}>
                    <img
                        className="w-12 h-12 min-w-[10px] min-h-[10px] rounded-full ml-2"
                        src={videos[0]?.channel.profilePicture}
                        alt="Profile Picture"
                    />
                </a>
                <h1 className="text-3xl font-bold dark:text-stone-100 mr-1">
                    {videos[0]?.channel.broadcasterName}
                </h1>
                <Button onClick={handleClick} disabled={isFetching} style={"svg"}>
                    <svg className="fill-current w-4 h-4" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">
                        <path d="M13 8V2H7v6H2l8 8 8-8h-5zM0 18h20v2H0v-2z" />
                    </svg>
                </Button>
            </div>
            <div className="mt-5">
                <h2 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Videos")}</h2>
                {hasVideos && <VideoComponent videos={videos} disablePicture={true} />}
            </div>
        </div>
    );
};

export default Channel;
