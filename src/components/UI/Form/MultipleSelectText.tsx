import { FC, useRef, useState } from "react";
import useOutsideClick from "../../../hooks/useOutsideClick";
import classNames from "classnames";

interface MultipleSelectTextProps {
    label?: string;
    id: string;
    error: any;
    options: string[];
    disabled?: boolean;
    placeholder: string;
    required?: boolean;
    onChannelChange: any;
    value?: string;
}

const MultipleSelectText: FC<MultipleSelectTextProps> = ({
    label,
    id,
    error,
    options,
    required,
    onChannelChange,
    placeholder,
    disabled = false,
    value,
}) => {
    const [channel, setChannel] = useState<string>(value || "");
    const [possibleMatches, setPossibleMatches] = useState<string[]>(options || []);
    const [showPopup, setShowPopup] = useState<boolean>(false);
    const popupRef = useRef(null);
    useOutsideClick(popupRef, () => setShowPopup(false));

    const togglePopup = () => {
        setShowPopup(!showPopup);
    };

    const handleInputChange = (newChannel: string) => {
        if (channel != newChannel) {
            const matches = options
                .filter((channel) => channel?.toLowerCase().startsWith(newChannel.toLowerCase()))
                .map((channel) => channel);
            setPossibleMatches(matches);
            setChannel(newChannel);
            onChannelChange(newChannel);
        }
    };

    const handleItemClick = (name: string) => {
        handleInputChange(name);
        setShowPopup(false);
    };

    return (
        <>
            {label && (
                <label className="mb-2 block text-sm font-medium text-gray-900 dark:text-white" htmlFor={id}>
                    {label}
                </label>
            )}

            <div className="mx-auto flex w-full flex-col ">
                <div className="w-full">
                    <div ref={popupRef} className=" items-cente relative flex flex-col ">
                        <div className="w-full">
                            <div
                                className={classNames(
                                    `flex h-12 w-full flex-wrap items-center rounded-lg border bg-gray-50 px-2.5 text-sm text-gray-900 focus:border-blue-500 focus:ring-blue-500 dark:border-custom_lightblue  dark:text-white dark:placeholder-gray-400 dark:focus:border-custom_lightblue dark:focus:ring-custom_vista_blue`,
                                    {
                                        "dark:bg-custom_blue": disabled,
                                        "dark:bg-custom_lightblue": !disabled,
                                    }
                                )}>
                                <div className="h-full flex-1">
                                    <input
                                        value={channel}
                                        type="text"
                                        onClick={() => togglePopup()}
                                        placeholder={placeholder}
                                        onChange={(e) => handleInputChange(e.target.value)}
                                        required={required}
                                        disabled={disabled}
                                        className="h-full w-full flex-1 rounded-lg border-0 border-none border-transparent bg-gray-50 p-0 text-sm text-gray-900 focus-within:border-0  focus-within:border-transparent focus-within:outline-none   focus:border-none  focus:ring-0 dark:bg-custom_lightblue dark:text-white  dark:placeholder-gray-400 disabled:dark:bg-custom_blue"
                                    />
                                </div>
                            </div>
                        </div>
                        {showPopup && (
                            <div className="z-80 absolute left-0 top-14 max-h-64 w-full overflow-y-auto rounded border border-custom_space_cadet_bis bg-white dark:bg-custom_lightblue">
                                <div className="flex w-full flex-col">
                                    {possibleMatches.map((name) => (
                                        <div
                                            key={name}
                                            className="w-full cursor-pointer border-b border-gray-100 dark:border-custom_lightblue"
                                            onClick={() => handleItemClick(name)}>
                                            <div className="relative flex w-full items-center border-l-2 border-transparent p-2 pl-2 hover:bg-custom_vista_blue">
                                                <div className="flex w-full items-center">
                                                    <div className="mx-2 leading-6 dark:text-white">{name}</div>
                                                </div>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            </div>
            {error && (
                <span className=" self-start rounded-md px-2 py-1 italic text-red-500">{error.message}</span>
            )}
        </>
    );
};

export default MultipleSelectText;
