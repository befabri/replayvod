import React from "react";
import ScheduleComponent from "../../components/Others/ScheduleForm";
import { useTranslation } from "react-i18next";
import Title from "../../components/Typography/TitleComponent";

const AddChannelPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <div className="mb-8 mt-20 md:mt-16">
            <div className="p-4 pb-8 md:p-7">
                <Title title={t("Schedule")} />
            </div>
            <ScheduleComponent />
        </div>
    );
};

export default AddChannelPage;
