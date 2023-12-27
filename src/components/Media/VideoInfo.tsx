import { FC, useState, useEffect, useRef } from "react";
import { CompletedVideo } from "../../type";
import { Pathnames } from "../../type/routes";
import { toKebabCase, truncateString } from "../../utils/utils";
import { Link } from "react-router-dom";
import HrefLink from "../UI/Navigation/HrefLink";

type VideoInfoProps = {
    video: CompletedVideo | undefined;
    disablePicture?: boolean;
};

const VideoInfoComponent: FC<VideoInfoProps> = ({ video, disablePicture = false }) => {
    const [divWidth, setDivWidth] = useState<number>(0);
    const divRef = useRef<HTMLDivElement>(null);

    const numberOfTagsToRender = Math.max(Math.floor(divWidth / 100 - 1), 2);

    useEffect(() => {
        const handleResize = () => {
            if (divRef.current) {
                setDivWidth(divRef.current.offsetWidth);
            }
        };

        handleResize();

        window.addEventListener("resize", handleResize);

        return () => {
            window.removeEventListener("resize", handleResize);
        };
    }, []);

    return (
        <div className="flex flex-row gap-3 mt-2" ref={divRef}>
            {!disablePicture && (
                <Link
                    to={`${Pathnames.Video.Channel}/${video?.displayName.toLowerCase()}`}
                    className="min-w-[40px]">
                    <img
                        className="w-10 h-10 rounded-full mt-1"
                        src={video?.channel.profilePicture}
                        alt="Profile Picture"
                    />
                </Link>
            )}
            <div>
                <div>
                    <HrefLink to={`${Pathnames.Watch}${video?.id}`} style="title">
                        {video?.titles[0].title.name}
                    </HrefLink>
                </div>
                <div>
                    <HrefLink to={`${Pathnames.Video.Channel}/${video?.displayName.toLowerCase()}`}>
                        {video?.channel.broadcasterName}
                    </HrefLink>
                </div>
                <div>
                    {video?.videoCategory.map((item) => (
                        <HrefLink
                            to={`${Pathnames.Video.Category}/${toKebabCase(item.category.name)}`}   
                            key={item.categoryId}>
                            {item.category.name}
                        </HrefLink>
                    ))}
                </div>
                <div>
                    {video?.tags.slice(0, numberOfTagsToRender).map((item) => (
                        <span
                            className="bg-gray-100 text-gray-800 text-xs font-medium mr-2 px-2.5 py-0.5 rounded-full dark:bg-custom_space_cadet dark:text-gray-400"
                            key={item.tag.name}>
                            {truncateString(item.tag.name, 18, false)}
                        </span>
                    ))}
                </div>
            </div>
        </div>
    );
};

export default VideoInfoComponent;
