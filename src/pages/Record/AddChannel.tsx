import React, { useState, useEffect } from "react";
import InputText from "../../components/InputText";
import Select from "../../components/Select";
import ToggleSwitch from "../../components/ToggleSwitch";
import InputNumber from "../../components/InputNumber";
import Button from "../../components/Button";
import { useTranslation } from "react-i18next";
import { Channel } from "../../type";

// Todo remove source -> provider et enlever toutes les chaines
const AddChannel: React.FC = () => {
    const { t } = useTranslation();
    const [source, setSource] = useState("Une chaine");
    const [channelName, setChannelName] = useState("");
    const [viewersCount, setViewersCount] = useState(0);
    const [timeBeforeDelete, setTimeBeforeDelete] = useState(0);
    const [trigger, setTrigger] = useState("trigger1");
    const [tag, setTag] = useState("tag1");
    const [category, setCategory] = useState("cat1");
    const [quality, setQuality] = useState("quality1");
    const [isDeleteRediff, setIsDeleteRediff] = useState(false);
    const [users, setUsers] = useState<Channel[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [possibleMatches, setPossibleMatches] = useState<string[]>([]);
    const [isValid, setIsValid] = useState(true);
    const ROOT_URL = import.meta.env.VITE_ROOTURL;
    const MIN_CHANNEL_LENGTH = 3;
    const CHECK_NAME_URL = `${ROOT_URL}/api/users/name/`;
    const GET_FOLLOWED_CHANNELS_URL = `${ROOT_URL}/api/users/me/followedchannels`;

    const checkChannelNameValidity = async (channelName: string) => {
        try {
            const response = await fetch(`${CHECK_NAME_URL}${channelName}`, {
                credentials: "include",
            });

            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || `HTTP error! status: ${response.status}`);
            }

            const data = await response.json();
            return data.exists;
        } catch (error) {
            console.error(`Error fetching data: ${error}`);
            return false;
        }
    };

    const handleBlur = async () => {
        if (channelName.length < MIN_CHANNEL_LENGTH) {
            return;
        }

        const exists = await checkChannelNameValidity(channelName);
        setIsValid(exists);
    };

    useEffect(() => {
        const fetchFollowedChannels = async () => {
            try {
                const response = await fetch(`${GET_FOLLOWED_CHANNELS_URL}`, {
                    credentials: "include",
                });

                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }

                const data = await response.json();
                setUsers(data);
                setIsLoading(false);
            } catch (error) {
                console.error(`Error fetching data: ${error}`);
            }
        };

        fetchFollowedChannels();
    }, []);

    const submit = async () => {
        const data = {
            source,
            channelName,
            viewersCount,
            timeBeforeDelete,
            trigger,
            tag,
            category,
            quality,
            isDeleteRediff,
        };

        try {
            const response = await fetch(`${ROOT_URL}/api/dl/channels`, {
                method: "POST",
                credentials: "include",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify(data),
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            // TODO
        } catch (error) {
            console.error(`Error posting data: ${error}`);
        }
    };

    const handleSourceChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
        setSource(event.target.value);
    };

    const handleChannelNameChange = (event: React.ChangeEvent<HTMLInputElement>) => {
        setChannelName(event.target.value);
        if (event.target.value.length > 0) {
            const matches = users
                .filter((user) => user.broadcasterName.toLowerCase().startsWith(event.target.value.toLowerCase()))
                .map((user) => user.broadcasterName);
            setPossibleMatches(matches);
            console.log(
                users
                    .filter((user) =>
                        user.broadcasterName.toLowerCase().startsWith(event.target.value.toLowerCase())
                    )
                    .map((user) => user.broadcasterName)
            );
        } else {
            setPossibleMatches([]);
        }
    };

    const handleViewersCountChange = (event: React.ChangeEvent<HTMLInputElement>) => {
        setViewersCount(Number(event.target.value));
    };

    const handleTimeBeforeDeleteChange = (event: React.ChangeEvent<HTMLInputElement>) => {
        setTimeBeforeDelete(Number(event.target.value));
    };

    const handleTriggerChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
        setTrigger(event.target.value);
    };

    const handleTagChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
        setTag(event.target.value);
    };

    const handleCategoryChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
        setCategory(event.target.value);
    };

    const handleQualityChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
        setQuality(event.target.value);
    };

    const handleDeleteRediffChange = () => {
        setIsDeleteRediff(!isDeleteRediff);
    };

    if (isLoading) {
        return <p>{t("Loading")}</p>;
    }

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Schedule Videos")}</h1>
                <div>
                    <Select
                        label={t("Select Provider")}
                        id={source}
                        value={source}
                        onChange={handleSourceChange}
                        options={[t("A channel"), t("All channels followed"), t("All channels")]}
                    />
                </div>
                {source === t("A channel") && (
                    <div className="mt-5">
                        <InputText
                            label={t("Channel Name")}
                            id="channelName"
                            onBlur={handleBlur}
                            isValid={isValid}
                            value={channelName.toLowerCase().replace(/ /g, "")}
                            onChange={handleChannelNameChange}
                            placeholder={t("Channel Name")}
                            required={true}
                            list="possible-matches"
                        />
                        <datalist id="possible-matches">
                            {possibleMatches.map((match, index) => (
                                <option key={index} value={match} />
                            ))}
                        </datalist>
                    </div>
                )}
                <div className="mt-5">
                    <Select
                        label={t("Select a trigger")}
                        id={trigger}
                        value={trigger}
                        onChange={handleTriggerChange}
                        options={[
                            t("Every time the channel goes live"),
                            t("By category"),
                            t("By Tags"),
                            t("By minimum number of views"),
                        ]}
                    />
                    {trigger === "trigger3" && (
                        <Select
                            label={t("Twitch tags")}
                            id={tag}
                            value={tag}
                            onChange={handleTagChange}
                            options={["tag 1", "tag 2", "tag 3"]}
                        />
                    )}
                    {trigger === "trigger2" && (
                        <Select
                            label={t("Live category")}
                            id={category}
                            value={category}
                            onChange={handleCategoryChange}
                            options={[t("Just Chatting"), "Diablo 4", "Elden Ring"]}
                        />
                    )}
                    {trigger === "trigger4" && (
                        <div className="mb-6">
                            <InputNumber
                                label={t("Minimum number of views")}
                                id="default-input"
                                value={viewersCount}
                                onChange={handleViewersCountChange}
                            />
                        </div>
                    )}
                    <div className="mt-5">
                        <Select
                            label={t("Video quality")}
                            id="quality"
                            value={quality}
                            onChange={handleQualityChange}
                            options={["1080p", "720p", "480p"]}
                        />
                    </div>
                    <div className="mt-5">
                        <ToggleSwitch
                            label={t("Deletion of the video if the VOD is kept after the stream")}
                            id="toggleB"
                            checked={isDeleteRediff}
                            onChange={handleDeleteRediffChange}
                        />

                        {isDeleteRediff === true && (
                            <div className="mb-6">
                                <InputNumber
                                    label={t("Set the stream end time in minutes before the VOD is suppressed")}
                                    id="timeBeforeDelete"
                                    value={timeBeforeDelete}
                                    onChange={handleTimeBeforeDeleteChange}
                                />
                            </div>
                        )}
                    </div>
                    <Button text={t("Submit")} onClick={submit} />
                </div>
            </div>
        </div>
    );
};

export default AddChannel;
