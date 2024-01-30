import { useTranslation } from "react-i18next";
import NotFound from "../components/Others/NotFound";
import Container from "../components/Layout/Container";

const NotFoundPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <Container>
            <div className="mt-14 p-4">
                <NotFound text={t("Sorry, the requested page could not be found.")} />
            </div>
        </Container>
    );
};

export default NotFoundPage;
