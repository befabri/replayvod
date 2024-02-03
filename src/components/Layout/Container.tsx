import { FC } from "react";

interface ContainerProps {
    children: React.ReactNode;
}

const Container: FC<ContainerProps> = ({ children }) => {
    return <div className="mb-4 mt-20 p-4 md:mt-16 md:p-7">{children}</div>;
};

export default Container;
