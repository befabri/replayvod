import { FC } from "react";
import Title from "../Typography/TitleComponent";
import Layout from "./Layout";

interface TitledLayoutProps {
    children: React.ReactNode;
    title: string;
}

const TitledLayout: FC<TitledLayoutProps> = ({ title, children }) => {
    return (
        <Layout>
            <div className="pb-8">
                <Title title={title} />
            </div>
            {children}
        </Layout>
    );
};

export default TitledLayout;
