# React Frontend for ReplayVod

This repository contains the frontend codebase for ReplayVod. It is built using React and is intended to be used alongside our [Fastify Backend](https://gitlab.com/replayvod/replay-vod-api).

## Table of Contents

-   [Prerequisites](#prerequisites)
-   [Development Setup](#development-setup)
-   [License](#license)

## Prerequisites

-   Node.js v18+
-   npm
-   Docker (for deployment)

## Development Setup

1. **Clone the Repository**

    ```bash
    git clone https://gitlab.com/replayvod/replay-vod-web.git
    cd replay-vod-web
    ```

2. **Install Dependencies**

    ```bash
    npm install
    ```

3. **Environment Variables**
   Create a `.env` file in the root directory and fill it with the necessary environment variables. We provide an .env.example file to illustrate the expected variables.

    ```bash
    VITE_ROOTURL="http://localhost:8080" # URL of the API (https://gitlab.com/replayvod/replay-vod-api)
    ```

4. **Run the Development Server**

    ```bash
    npm run dev
    ```

    This will start the Fastify server in development mode, watching for any changes.

## License

This project is licensed under the GNU General Public License v3.0. - see the [LICENSE.md](https://gitlab.com/replayvod/replay-vod-web/-/blob/main/README.md) file for details.
