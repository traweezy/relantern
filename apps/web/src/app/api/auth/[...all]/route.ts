import type { NextRequest } from "next/server";
import { getAuth } from "@/server/auth/auth";

const handle = (request: NextRequest): Promise<Response> => getAuth().handler(request);

export const GET = handle;
export const POST = handle;
