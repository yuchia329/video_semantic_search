import { NextRequest, NextResponse } from "next/server";

const BACKEND = process.env.BACKEND_URL ?? "http://localhost:8080";

// Proxy: redirect browser to Go's /stream which then redirects to MinIO pre-signed URL.
// The client follows both redirects and can do Range requests directly against MinIO.
export async function GET(req: NextRequest) {
  const id = req.nextUrl.searchParams.get("id") ?? "";
  if (!id) return new NextResponse("id required", { status: 400 });

  // Ask Go to generate a pre-signed URL and redirect there.
  const res = await fetch(`${BACKEND}/stream?id=${encodeURIComponent(id)}`, {
    redirect: "manual",
  });

  if (res.status === 307 || res.status === 302 || res.status === 301) {
    const location = res.headers.get("location");
    if (location) return NextResponse.redirect(location, { status: 307 });
  }

  return new NextResponse(await res.text(), { status: res.status });
}
