import React from "react";
import { useTranslation } from "react-i18next";
import Table from "../../components/Table/Tables";
import { Video } from "../../type";
import { ApiRoutes } from "../../type/routes";
import { customFetch } from "../../utils/utils";
import { useQuery } from "@tanstack/react-query";
import TitledLayout from "../../components/Layout/TitledLayout";

const HistoryPage: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: videos,
        isLoading,
        isError,
        error,
    } = useQuery<Video[], Error>({
        queryKey: ["videos", "all"],
        queryFn: (): Promise<Video[]> => customFetch(ApiRoutes.GET_VIDEO_ALL),
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !videos) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <TitledLayout title={t("History")}>
            {isLoading ? (
                <div>{t("Loading")}</div>
            ) : (
                <Table items={videos} showEdit={false} showCheckbox={false} showId={false} />
            )}
        </TitledLayout>
    );
};

export default HistoryPage;
