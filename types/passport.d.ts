export {};

declare module "express-session" {
  interface SessionData {
    passport: { [key: string]: any };
  }
}
