/** @type {import('tailwindcss').Config} */
export default {
    content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
    darkMode: "class",
    theme: {
        extend: {
            colors: {
                custom_green: "#AAA95A",
                custom_violet: "#2F195F",
                custom_black: "#0E0D19",
                custom_blue: "#151425",
                custom_space_cadet: "#262444",
                custom_lightblue: "#1C1A31",
                custom_lime: "#CEFF1A",
                custom_cream: "#F1DAC4",
                custom_space_cadet_bis: "#33305E",
                custom_vista_blue: "#8390FA",
                custom_yellow: "#FAC748",
                custom_aquamarine: "#38fcbb",
                custom_delft_blue: "#3C3869",
                custom_azure: "#EDF7F6",
            },
            // eslint-disable-next-line @typescript-eslint/no-unused-vars
            typography: (_theme) => ({
                DEFAULT: {
                    css: {
                        ".truncate-multiline": {
                            display: "-webkit-box",
                            "-webkit-line-clamp": "2",
                            "-webkit-box-orient": "vertical",
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                        },
                    },
                },
            }),
        },
    },
    // eslint-disable-next-line no-undef
    plugins: [require("@tailwindcss/forms"), require("@tailwindcss/typography"), require("tailwind-scrollbar")],
};
