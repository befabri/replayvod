import { OAuth2Namespace } from "@fastify/oauth2";
import { FastifyInstance } from "fastify";
import { UserSession } from "../models/userModel";

declare module "@fastify/secure-session" {
    interface SessionData {
        user: UserSession;
    }
}

declare module "fastify" {
    interface FastifyInstance {
        twitchOauth2: OAuth2Namespace;
    }
}
