import React from "react";
import { useTranslation } from "react-i18next";
import Table from "../../components/Table/Tables";
import { Video } from "../../type";
import { ApiRoutes } from "../../type/routes";
import { customFetch } from "../../utils/utils";
import { useQuery } from "@tanstack/react-query";

const QueuePage: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: videos,
        isLoading,
        isError,
        error,
    } = useQuery<Video[], Error>({
        queryKey: ["videos", "pending"],
        queryFn: (): Promise<Video[]> => customFetch(ApiRoutes.GET_VIDEO_PENDING),
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !videos) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <div className="p-4">
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Queue")}</h1>
            </div>
            {isLoading ? (
                <div>{t("Loading")}</div>
            ) : (
                <Table items={videos} showEdit={false} showCheckbox={false} showId={false} />
            )}
        </div>
    );
};

export default QueuePage;
