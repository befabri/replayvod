import React from "react";
import { useTranslation } from "react-i18next";
import TableTasks from "../../components/Table/TableTasks";
import { Task } from "../../type";
import { ApiRoutes } from "../../type/routes";
import { customFetch } from "../../utils/utils";
import { useQuery } from "@tanstack/react-query";
import TitledLayout from "../../components/Layout/TitledLayout";

const TasksPage: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: tasks,
        isLoading,
        isError,
        error,
    } = useQuery<Task[], Error>({
        queryKey: ["tasks"],
        queryFn: (): Promise<Task[]> => customFetch(ApiRoutes.GET_TASK),
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !tasks) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <TitledLayout title={t("Tasks Scheduled")}>
            {isLoading ? <div>{t("Loading")}</div> : <TableTasks items={tasks} />}
        </TitledLayout>
    );
};

export default TasksPage;
