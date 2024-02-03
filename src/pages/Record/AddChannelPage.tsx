import React from "react";
import ScheduleComponent from "../../components/Others/ScheduleForm";
import { useTranslation } from "react-i18next";
import TitledLayout from "../../components/Layout/TitledLayout";

const AddChannelPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <TitledLayout title={t("Schedule")}>
            <ScheduleComponent />
        </TitledLayout>
    );
};

export default AddChannelPage;
