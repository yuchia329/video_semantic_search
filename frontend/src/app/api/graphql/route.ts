import { NextRequest, NextResponse } from "next/server";

const BACKEND = process.env.BACKEND_URL ?? "http://localhost:8080";

export async function POST(req: NextRequest) {
  const body = await req.text();
  const uid  = req.headers.get("X-User-ID") ?? "";
  const res = await fetch(`${BACKEND}/query`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-User-ID": uid },
    body,
  });
  const json = await res.json();
  return NextResponse.json(json);
}
