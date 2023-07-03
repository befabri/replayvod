/** @type {import('tailwindcss').Config} */
export default {
    content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
    darkMode: "class",
    theme: {
        extend: {
            typography: (theme) => ({
                DEFAULT: {
                    css: {
                        // ...
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
    plugins: [require("@tailwindcss/forms"), require("@tailwindcss/typography")],
};
