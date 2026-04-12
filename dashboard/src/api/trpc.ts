import { createTRPCContext } from "@trpc/tanstack-react-query"

// TODO: Import generated AppRouter type once trpcgo generates it
// import type { AppRouter } from "./generated/trpc"
// biome-ignore lint/suspicious/noExplicitAny: placeholder until codegen
type AppRouter = any

export const { TRPCProvider, useTRPC } = createTRPCContext<AppRouter>()
