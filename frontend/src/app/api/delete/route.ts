import { NextRequest, NextResponse } from "next/server";

const BACKEND = process.env.BACKEND_URL ?? "http://localhost:8080";

// DELETE or POST — proxy to Go's /delete?id=<id>
export async function DELETE(req: NextRequest) {
  const id  = req.nextUrl.searchParams.get("id") ?? "";
  const uid = req.headers.get("X-User-ID") ?? "";
  const res = await fetch(`${BACKEND}/delete?id=${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: { "X-User-ID": uid },
  });
  if (res.status === 204) return new NextResponse(null, { status: 204 });
  const text = await res.text();
  return new NextResponse(text, { status: res.status });
}
