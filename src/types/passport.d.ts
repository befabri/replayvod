import session from "express-session";
import express from "express";

declare module "express-session" {
  interface SessionData extends session.SessionData {
    passport: { [key: string]: any };
  }
}

declare module "express-serve-static-core" {
  interface Request extends express.Request {
    session: any;
  }
}
