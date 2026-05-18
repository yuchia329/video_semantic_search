import { NextRequest, NextResponse } from "next/server";

const BACKEND = process.env.BACKEND_URL ?? "http://localhost:8080";

export const dynamic = 'force-dynamic';

export async function GET(req: NextRequest) {
  const uid = req.nextUrl.searchParams.get("uid") || "";
  const res = await fetch(`${BACKEND}/status-stream`, {
    headers: { "X-User-ID": uid },
  });

  return new NextResponse(res.body, {
    status: res.status,
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      "Connection": "keep-alive",
    },
  });
}
