import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Table from "../../components/Table/Tables";
import { Video } from "../../type";
import { ApiRoutes, getApiRoute } from "../../type/routes";

const QueuePage: React.FC = () => {
    const { t } = useTranslation();
    const [videos, setVideos] = useState<Video[]>([]);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            const url = getApiRoute(ApiRoutes.GET_VIDEO_ALL);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            const convertedVideos = data.filter((video: Video) => video.status === "PENDING");
            setVideos((prevVideos) => {
                const updatedVideos = prevVideos.map((video) => {
                    const newVideo = convertedVideos.find((v: { id: number }) => v.id === video.id);
                    return newVideo || video;
                });
                return [
                    ...updatedVideos,
                    ...convertedVideos.filter(
                        (v: { id: number }) => !prevVideos.find((video) => v.id === video.id)
                    ),
                ];
            });
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 10000);
        return () => clearInterval(intervalId);
    }, []);

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Queue")}</h1>
            </div>
            {isLoading ? (
                <div>{t("Loading")}</div>
            ) : (
                <Table items={videos} showEdit={false} showCheckbox={false} />
            )}
        </div>
    );
};

export default QueuePage;
