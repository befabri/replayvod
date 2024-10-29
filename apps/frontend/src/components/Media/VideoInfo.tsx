import { FC, useState, useEffect, useRef } from "react";
import { CompletedVideo } from "../../type";
import { Pathnames } from "../../type/routes";
import { toKebabCase, truncateString } from "../../utils/utils";
import { Link } from "react-router-dom";
import HrefLink from "../UI/Navigation/HrefLink";
import ProfileImage from "../Profile/ProfileImage";

type VideoInfoProps = {
    video: CompletedVideo;
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
        <div className="mt-2 flex flex-row gap-3" ref={divRef}>
            {!disablePicture && (
                <Link
                    to={`${Pathnames.Video.Channel}/${video?.displayName.toLowerCase()}`}
                    className="min-w-[40px]">
                    <ProfileImage url={video.channel.profilePicture} height={"10"} width={"10"} className="mt-1" />
                </Link>
            )}
            <div>
                <div title={video?.titles[0]}>
                    <HrefLink to={`${Pathnames.Watch}${video?.id}`} style="title">
                        {truncateString(video?.titles[0], 125, true)}
                    </HrefLink>
                </div>
                <div>
                    <HrefLink to={`${Pathnames.Video.Channel}/${video?.displayName.toLowerCase()}`}>
                        {video?.channel.broadcasterName}
                    </HrefLink>
                </div>
                <div>
                    {video?.videoCategory.map((item) => (
                        <HrefLink to={`${Pathnames.Video.Category}/${toKebabCase(item.name)}`} key={item.id}>
                            {item.name}
                        </HrefLink>
                    ))}
                </div>
                <div>
                    {video?.tags.slice(0, numberOfTagsToRender).map((item) => (
                        <span
                            className="mr-2 rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-medium text-gray-800 dark:bg-custom_space_cadet dark:text-gray-400"
                            key={item}>
                            {truncateString(item, 18, false)}
                        </span>
                    ))}
                </div>
            </div>
        </div>
    );
};

export default VideoInfoComponent;
