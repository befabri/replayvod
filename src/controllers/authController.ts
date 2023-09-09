import { FastifyReply, FastifyRequest } from "fastify";
import axios from "axios";

export async function handleTwitchAuth(req: FastifyRequest, reply: FastifyReply): Promise<void> {}

export async function handleTwitchCallback(req: FastifyRequest, reply: FastifyReply): Promise<void> {}

export async function checkSession(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    if (req.session?.passport?.user) {
        reply.status(200).send({ status: "authenticated" });
    } else {
        reply.status(200).send({ status: "not authenticated" });
    }
}

export async function getUser(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    if (req.session?.passport?.user) {
        const { accessToken, refreshToken, ...user } = req.session.passport.user;
        reply.send(user);
    } else {
        reply.status(401).send({ error: "Unauthorized" });
    }
}

export async function refreshToken(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    if (req.session?.passport?.user?.refreshToken) {
        const refreshToken = req.session.passport.user.refreshToken;
        const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;
        const TWITCH_SECRET = process.env.TWITCH_SECRET;

        try {
            const response = await axios({
                method: "post",
                url: "https://id.twitch.tv/oauth2/token",
                params: {
                    grant_type: "refresh_token",
                    refresh_token: refreshToken,
                    client_id: TWITCH_CLIENT_ID,
                    client_secret: TWITCH_SECRET,
                },
            });

            if (response.status === 200) {
                req.session.passport.user.accessToken = response.data.access_token;
                req.session.passport.user.refreshToken = response.data.refresh_token;

                reply.status(200).send({ status: "Token refreshed" });
            } else {
                reply.status(500).send({ error: "Failed to refresh token" });
            }
        } catch (error) {
            console.error(`Failed to refresh token: ${error}`);
            reply.status(500).send({ error: "Failed to refresh token" });
        }
    } else {
        reply.status(401).send({ error: "Unauthorized" });
    }
}
