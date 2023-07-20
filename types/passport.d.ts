export {};

declare module "express-session" {
  interface SessionData {
    passport: { [key: string]: any };
  }
}

declare module "express" {
  export interface Request {
    session: any;
  }
}
