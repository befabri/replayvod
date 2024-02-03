import React from "react";
import { useTranslation } from "react-i18next";
import TableLogs from "../../components/Table/TableLogs";
import { Log } from "../../type";
import { ApiRoutes } from "../../type/routes";
import { useQuery } from "@tanstack/react-query";
import { customFetch } from "../../utils/utils";
import TitledLayout from "../../components/Layout/TitledLayout";

const LogsPage: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: log,
        isLoading,
        isError,
        error,
    } = useQuery<Log[], Error>({
        queryKey: ["log"],
        queryFn: (): Promise<Log[]> => customFetch(ApiRoutes.GET_LOG_FILES),
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !log) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <TitledLayout title={t("Log Files")}>
            {isLoading ? <div>{t("Loading")}</div> : <TableLogs items={log} />}
        </TitledLayout>
    );
};

export default LogsPage;
