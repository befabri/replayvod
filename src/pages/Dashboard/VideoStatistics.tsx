import React, { useState, useEffect } from "react";
import { ApiRoutes, getApiRoute } from "../../type/routes";
import { useTranslation } from "react-i18next";

const VideoStatisticsPage: React.FC = () => {
    const { t } = useTranslation();
    const [stats, setStats] = useState({ totalDoneVideos: 0, totalDownloadingVideos: 0, totalFailedVideos: 0 });
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            const url = getApiRoute(ApiRoutes.GET_VIDEO_STATISTICS);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            setStats(data);
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 10000);

        return () => clearInterval(intervalId);
    }, []);

    return (
        <div className="rounded-lg bg-white p-4 shadow  dark:bg-custom_lightblue sm:p-5">
            <h5 className="mb-4 text-xl font-medium text-gray-500 dark:text-white">Vidéos</h5>
            <div className="flex flex-col text-gray-900 dark:text-white">
                <span className="text-3xl font-bold tracking-tight">{stats.totalDoneVideos}</span>
                <span className="text-xl font-normal text-gray-500 dark:text-gray-400">Téléchargées</span>
                <span className="mt-2 text-3xl font-bold tracking-tight">{stats.totalDownloadingVideos}</span>
                <span className="text-xl font-normal text-gray-500 dark:text-gray-400">
                    En cours de téléchargement
                </span>
                <span className="mt-2 text-3xl font-bold tracking-tight">{stats.totalFailedVideos}</span>
                <span className="text-xl font-normal text-gray-500 dark:text-gray-400">Échouées</span>
            </div>
        </div>
    );
};

export default VideoStatisticsPage;
