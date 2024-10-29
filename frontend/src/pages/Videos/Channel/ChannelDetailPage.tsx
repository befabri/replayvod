import React from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ChannelDetailResponse, CompletedVideo } from "../../../type";
import VideoComponent from "../../../components/Media/Video";
import { ApiRoutes, getApiRoute } from "../../../type/routes";
import NotFound from "../../../components/Others/NotFound";
import { useQuery } from "@tanstack/react-query";
import Button from "../../../components/UI/Button/Button";
import Layout from "../../../components/Layout/Layout";
import Title from "../../../components/Typography/TitleComponent";
import ProfileImage from "../../../components/Profile/ProfileImage";

const fetchImage = async (url: RequestInfo | URL): Promise<string> => {
    const response = await fetch(url, { credentials: "include" });
    const blob = await response.blob();
    return URL.createObjectURL(blob);
};

const fetchChannelVideos = async (id: string): Promise<ChannelDetailResponse> => {
    const url = getApiRoute(ApiRoutes.GET_VIDEO_CHANNEL_BROADCASTERLOGIN, "broadcasterLogin", id);
    const response = await fetch(url, { credentials: "include" });
    if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
    }

    const { videos, channel } = await response.json();

    if (videos.length === 0) {
        return { channel: channel, videos: [] };
    }
    const videosWithBlobUrls = await Promise.all(
        videos.map(async (video: CompletedVideo) => {
            const thumbnailUrl = getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", video.thumbnail);
            const imageUrl = await fetchImage(thumbnailUrl);
            return { ...video, thumbnail: imageUrl };
        })
    );
    return { channel, videos: videosWithBlobUrls };
};

const ChannelDetailPage: React.FC = () => {
    const { t } = useTranslation();
    const { id } = useParams<{ id: string }>();

    if (!id) {
        throw new Error("No channel ID provided");
    }

    const { data, isLoading, isError } = useQuery<ChannelDetailResponse, Error>({
        queryKey: ["channel", id],
        queryFn: () => fetchChannelVideos(id),
        staleTime: 5 * 60 * 1000,
        retry: false,
    });

    const handleClick = async (broadcasterId: string) => {
        if (!broadcasterId) {
            return;
        }
        const url = getApiRoute(ApiRoutes.GET_DOWNLOAD_STREAM_ID, "id", broadcasterId);
        try {
            const response = await fetch(url, { credentials: "include" });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            await response.json();
        } catch (error) {
            console.error("Error:", error);
        }
    };

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (!data || isError) {
        return (
            <Layout>
                <div className="mt-14 flex flex-row items-center gap-3 p-4"></div>
                <div className="mt-5">
                    <h2 className="pb-5 text-2xl font-bold dark:text-stone-100">{t("Videos")}</h2>
                    <NotFound text={t("Channel not found.")} />;
                </div>
            </Layout>
        );
    }

    const { channel, videos } = data;

    if (videos.length === 0) {
        return (
            <Layout>
                <div className="flex flex-row items-center gap-3">
                    <ProfileImage url={data?.channel.profilePicture} height={"12"} width={"12"} />
                    <Title title={data?.channel.broadcasterName} />
                    <Button onClick={() => handleClick(data.channel.broadcasterId)} style={"svg"}>
                        <svg
                            className="h-4 w-4 fill-current"
                            xmlns="http://www.w3.org/2000/svg"
                            viewBox="0 0 20 20">
                            <path d="M13 8V2H7v6H2l8 8 8-8h-5zM0 18h20v2H0v-2z" />
                        </svg>
                    </Button>
                </div>
                <div className="mt-5">
                    <h2 className="pb-5 text-2xl font-bold dark:text-stone-100">{t("Videos")}</h2>
                    <NotFound text={t("No video found.")} />;
                </div>
            </Layout>
        );
    }

    return (
        <Layout>
            <div className="flex flex-row items-center gap-3 pb-8">
                <ProfileImage url={channel.profilePicture} height={"12"} width={"12"} />
                <Title title={channel.broadcasterName} />
                <Button onClick={() => handleClick(channel.broadcasterId)} style={"svg"}>
                    <svg className="h-4 w-4 fill-current" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">
                        <path d="M13 8V2H7v6H2l8 8 8-8h-5zM0 18h20v2H0v-2z" />
                    </svg>
                </Button>
            </div>
            <div>
                <h2 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Videos")}</h2>
                {videos && <VideoComponent videos={videos} disablePicture={true} />}
            </div>
        </Layout>
    );
};

export default ChannelDetailPage;
