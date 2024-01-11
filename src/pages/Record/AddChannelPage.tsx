import React from "react";
import ScheduleComponent from "../../components/Others/ScheduleForm";
import { useTranslation } from "react-i18next";

const AddChannelPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <div className="p-4">
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Schedule")}</h1>
                <div className="mt-5">
                    <ScheduleComponent />
                </div>
            </div>
        </div>
    );
};

export default AddChannelPage;
