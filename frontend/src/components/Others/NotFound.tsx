import { FC } from "react";

interface NotFoundProps {
    text: string;
}

const NotFound: FC<NotFoundProps> = ({ text }) => {
    return <h3 className="py-8 text-center text-lg font-medium italic dark:text-gray-400">{text}</h3>;
};

export default NotFound;
