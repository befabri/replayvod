import React from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { CompletedVideo } from "../../type";
import VideoInfoComponent from "../../components/Media/VideoInfo";
import { ApiRoutes, getApiRoute } from "../../type/routes";
import NotFound from "../../components/Others/NotFound";

interface FetchVideoQueryKey {
    queryKey: [string, string | undefined];
}

const WatchPage: React.FC = () => {
    const { t } = useTranslation();
    const { id } = useParams();

    const fetchVideo = async ({ queryKey }: FetchVideoQueryKey): Promise<CompletedVideo> => {
        const [_, videoId] = queryKey;
        const videoUrl = getApiRoute(ApiRoutes.GET_VIDEO_ID, "id", videoId);
        const videoResponse = await fetch(videoUrl, { credentials: "include" });
        if (!videoResponse.ok) {
            throw new Error(`HTTP error! status: ${videoResponse.status}`);
        }
        const video = await videoResponse.json();

        const thumbnailUrl = getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", video.thumbnail);
        const thumbnailResponse = await fetch(thumbnailUrl, { credentials: "include" });
        if (!thumbnailResponse.ok) {
            throw new Error(`HTTP error! status: ${thumbnailResponse.status}`);
        }
        const thumbnailBlob = await thumbnailResponse.blob();

        return {
            ...video,
            thumbnail: URL.createObjectURL(thumbnailBlob),
            playUrl: getApiRoute(ApiRoutes.GET_VIDEO_PLAY_ID, "id", video.id),
        };
    };

    const {
        data: video,
        isLoading,
        isError,
    } = useQuery({
        queryKey: ["video", id],
        queryFn: fetchVideo,
        staleTime: 5 * 60 * 1000,
        retry: false,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !video) {
        return (
            <div className="p-4">
                <div className="mt-14 p-4">
                    <NotFound text={t("No video found.")} />
                </div>
            </div>
        );
    }

    return (
        <div className="flex h-screen flex-col items-center">
            <video
                controls
                controlsList="nodownload"
                poster={video.thumbnail}
                preload="auto"
                className="mx-auto mt-14 block max-h-screen w-full flex-grow overflow-auto bg-black object-contain">
                <source src={video.playUrl} type="video/mp4" />
                {t("Your browser does not support the video tag.")}
            </video>

            <div className="mb-4 ml-4 self-start">
                <VideoInfoComponent video={video} />
            </div>
        </div>
    );
};

export default WatchPage;
