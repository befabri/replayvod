import { OAuth2Namespace } from "@fastify/oauth2";
import { FastifyInstance } from "fastify";

declare module "fastify" {
    interface FastifyInstance {
        twitchOauth2: OAuth2Namespace;
    }
}
