import React, { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../type";
import VideoComponent from "../components/Video";
import { ApiRoutes, getApiRoute } from "../type/routes";

const ChannelPage: React.FC = () => {
    const { t } = useTranslation();
    let { id } = useParams();

    const [isLoading, setIsLoading] = useState<boolean>(true);
    const [isFetching, setIsFetching] = useState<boolean>(false);
    const [buttonText, setButtonText] = useState<string>("Enregistrer");
    const [videos, setVideos] = useState<CompletedVideo[]>([]);

    const handleClick = () => {
        if (!isFetching) {
            setIsFetching(true);
            setButtonText("En cours");
            let url = getApiRoute(ApiRoutes.GET_DOWNLOAD_STREAM_ID, "id", id);
            fetch(url, {
                credentials: "include",
            })
                .then((response) => response.json())
                .then((data) => {
                    console.log(data);
                    setIsFetching(false);
                    setButtonText("est enregistrer");
                })
                .catch((error) => {
                    console.error("Error:", error);
                    setIsFetching(false);
                });
        }
    };

    useEffect(() => {
        const fetchData = async () => {
            try {
                let url = getApiRoute(ApiRoutes.GET_VIDEO_USER_ID, "id", id);
                const videoResponse = await fetch(url, { credentials: "include" });
                const videosData = await videoResponse.json();

                const thumbnails = await Promise.all(
                    videosData.map((video: CompletedVideo) => {
                        url = getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", video.thumbnail);
                        return fetch(url, {
                            credentials: "include",
                        }).then((res) => res.blob());
                    })
                );

                const updatedVideos = videosData.map((video: CompletedVideo, index: number) => ({
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
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">
                    {videos[0]?.channel.broadcasterName}
                </h1>
            </div>
            <button
                onClick={handleClick}
                disabled={isFetching}
                className="bg-gray-300 hover:bg-gray-400 text-gray-800 font-bold py-1 px-3 rounded inline-flex items-center">
                <svg className="fill-current w-4 h-4 mr-2" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">
                    <path d="M13 8V2H7v6H2l8 8 8-8h-5zM0 18h20v2H0v-2z" />
                </svg>
                <span>{buttonText}</span>
            </button>
            <VideoComponent videos={videos} disablePicture={true} />
        </div>
    );
};

export default ChannelPage;
