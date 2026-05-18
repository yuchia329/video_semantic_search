import { NextRequest, NextResponse } from "next/server";

const BACKEND = process.env.BACKEND_URL ?? "http://localhost:8080";

export async function POST(req: NextRequest) {
  const form = await req.formData();
  const uid  = req.headers.get("X-User-ID") ?? "";
  const res = await fetch(`${BACKEND}/upload`, {
    method: "POST",
    headers: { "X-User-ID": uid },
    body: form,
  });
  const text = await res.text();
  if (!res.ok) return new NextResponse(text, { status: res.status });
  return new NextResponse(text, { status: res.status, headers: { "Content-Type": "application/json" } });
}
