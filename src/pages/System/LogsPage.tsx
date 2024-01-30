import React from "react";
import { useTranslation } from "react-i18next";
import TableLogs from "../../components/Table/TableLogs";
import { Log } from "../../type";
import { ApiRoutes } from "../../type/routes";
import { useQuery } from "@tanstack/react-query";
import { customFetch } from "../../utils/utils";

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
        <div className="p-4">
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Log Files")}</h1>
            </div>
            {isLoading ? <div>{t("Loading")}</div> : <TableLogs items={log} />}
        </div>
    );
};

export default LogsPage;
