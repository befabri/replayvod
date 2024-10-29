import React from "react";
import { ApiRoutes } from "../../type/routes";
import { useTranslation } from "react-i18next";
import { customFetch } from "../../utils/utils";
import { useQuery } from "@tanstack/react-query";

interface Statistic {
    totalDoneVideos: number;
    totalDownloadingVideos: number;
    totalFailedVideos: number;
}

const VideoStatisticsPage: React.FC = () => {
    const { t } = useTranslation();

    const { data, isLoading, isError, error } = useQuery<Statistic, Error>({
        queryKey: ["statistics"],
        queryFn: (): Promise<Statistic> => customFetch(ApiRoutes.GET_VIDEO_STATISTICS),
        staleTime: 5 * 60 * 1000,
    });

    const defaultStats = { totalDoneVideos: 0, totalDownloadingVideos: 0, totalFailedVideos: 0 };
    const stats = data || defaultStats;

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError) {
        return <div>Error: {error?.message}</div>;
    }

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
