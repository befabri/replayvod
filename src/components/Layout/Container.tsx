import { FC } from "react";

interface ContainerProps {
    children: React.ReactNode;
}

const Container: FC<ContainerProps> = ({ children }) => {
    return <div className="p-4">{children}</div>;
};

export default Container;
