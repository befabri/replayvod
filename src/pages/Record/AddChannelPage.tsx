import React from "react";
import ScheduleComponent from "../../components/Others/ScheduleForm";
import { useTranslation } from "react-i18next";
import Title from "../../components/Typography/TitleComponent";
import Container from "../../components/Layout/Container";

const AddChannelPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <Container>
            <Title title={t("Schedule")} />
            <ScheduleComponent />
        </Container>
    );
};

export default AddChannelPage;
