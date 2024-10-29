import { useTranslation } from "react-i18next";
import NotFound from "../components/Others/NotFound";
import Layout from "../components/Layout/Layout";

const NotFoundPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <Layout>
            <div className="mt-14 p-4">
                <NotFound text={t("Sorry, the requested page could not be found.")} />
            </div>
        </Layout>
    );
};

export default NotFoundPage;
