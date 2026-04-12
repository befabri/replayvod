import { createTRPCContext } from "@trpc/tanstack-react-query"
import type { AppRouter } from "./generated/trpc"

export const { TRPCProvider, useTRPC } = createTRPCContext<AppRouter>()

export type { AppRouter, RouterInputs, RouterOutputs } from "./generated/trpc"
