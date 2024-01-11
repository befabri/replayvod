import { useTranslation } from "react-i18next";
import NotFound from "../components/Others/NotFound";

const NotFoundPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <div className="p-4">
            <div className="mt-14 p-4">
                <NotFound text={t("Sorry, the requested page could not be found.")} />
            </div>
        </div>
    );
};

export default NotFoundPage;
