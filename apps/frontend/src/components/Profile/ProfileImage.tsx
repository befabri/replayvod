import { FC } from "react";

interface ProfileImageProps {
    url: string;
    height: string;
    width: string;
    status?: boolean;
    className?: string;
}

const ProfileImage: FC<ProfileImageProps> = ({
    url,
    height = "12",
    width = "12",
    status = false,
    className = "",
}) => {
    return (
        <div className="relative">
            <img className={`h-${height} w-${width} rounded-full ${className} `} src={url} alt="Profile Picture" />
            <span
                className={`absolute bottom-0 left-7 h-3.5 w-3.5 rounded-full ${
                    status ? "bg-red-600" : ""
                }  dark:border-gray-800`}></span>
        </div>
    );
};

export default ProfileImage;
