# Fastify Backend for ReplayVod

This repository contains the backend codebase for ReplayVod. It is built using Fastify and is intended to be used alongside our [React Frontend](https://gitlab.com/replayvod/replay-vod-web).

## Table of Contents

-   [Prerequisites](#prerequisites)
-   [Development Setup](#development-setup)
-   [Secure Session Secret](#secure-session-secret)
-   [Twitch Integration Setup](#twitch-integration-setup)
-   [License](#license)

## Prerequisites

-   Node.js v18+
-   npm
-   Docker (for deployment)
-   TWITCH_CLIENT_ID / TWITCH_SECRET

## Development Setup

1. **Clone the Repository**

    ```bash
    git clone https://gitlab.com/replayvod/replay-vod-api.git
    cd replay-vod-api
    ```

2. **Install Dependencies**

    ```bash
    npm install
    ```

3. **Environment Variables**
   Create a `.env` file in the root directory and fill it with the necessary environment variables. We provide an .env.example file to illustrate the expected variables.

4. **Run the Development Server**

    ```bash
    npm run dev
    ```

    This will start the Fastify server in development mode, watching for any changes.

## Secure Session Secret

To secure your sessions, you'll need a `secret-key`. Follow the steps below to generate and place this key in the appropriate directory:

1. **Generate the Secret Key**:

    - For most platforms:

        ```sh
        npx @fastify/secure-session > secret-key
        ```

    - If running in Windows Powershell:

        ```sh
        npx @fastify/secure-session | Out-File -Encoding default -NoNewline -FilePath secret-key
        ```

    If you haven't previously used this module with npx, you might be prompted to install it. Note that with the output redirect, this can cause the command to wait indefinitely for input.

    Alternatively, if you don't want to use `npx`, you can generate the `secret-key` by first installing the `@fastify/secure-session` library with your preferred package manager, and then:

    ```sh
    ./node_modules/@fastify/secure-session/genkey.js > secret-key
    ```

2. **Place the Secret Key in the 'secret' Folder**:

    After generating the `secret-key`, move it to the `secret` directory in your project:

    ```sh
    mv secret-key secret/secret-key
    ```

    If you're on Windows, you can use:

    ```sh
    move secret-key secret\secret-key
    ```

Make sure the `secret` directory exists in your project, or create it before moving the `secret-key`.

## Twitch Integration Setup

To integrate the backend with Twitch functionalities, you'll need to register your application with Twitch and obtain your personal `TWITCH_CLIENT_ID` and `TWITCH_SECRET`.
Follow the steps below to set up the Twitch integration:

1. **Register Your Application on Twitch**

    - Visit the [Twitch Developers Console](https://dev.twitch.tv/console).
    - Click on the "Applications" tab.
    - Select "Register Your Application".
    - Fill in the required details. For the `OAuth Redirect URLs`, you'll need to specify the callback URL your application uses.

        ```
        http://localhost:8080/api/auth/twitch/callback
        ```

        (Adjust the above URL if you're setting up a production environment or if your local setup uses a different port or path.)

    - Once registered, you'll be provided with a `Client ID` and a `Client Secret`.

2. **Update Environment Variables in .env**

    With your `Client ID` and `Client Secret` from Twitch, update the `.env` file (or create one based on the `.env.example` provided):

    ```env
    TWITCH_CLIENT_ID=your_client_id_from_twitch
    TWITCH_SECRET=your_client_secret_from_twitch
    CALLBACK_URL=your_callback_url
    ```

## License

This project is licensed under the GNU General Public License v3.0. - see the [LICENSE.md](https://gitlab.com/replayvod/replay-vod-api/-/blob/main/README.md) file for details.
