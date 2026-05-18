"use client";

import { useState, useRef, useCallback, useEffect } from "react";
import { v4 as uuidv4 } from "uuid";
import ReactMarkdown from "react-markdown";

// ─── User ID cookie ────────────────────────────────────────────────────────────
function getOrCreateUserID(): string {
  if (typeof document === "undefined") return "anonymous";
  const key = "vsearch_uid";
  let uid = document.cookie
    .split("; ")
    .find((r) => r.startsWith(key + "="))
    ?.split("=")[1];
  if (!uid) {
    uid = uuidv4();
    document.cookie = `${key}=${uid}; path=/; max-age=${60 * 60 * 24 * 365}; SameSite=Lax`;
  }
  return uid;
}

// ─── GraphQL helpers ───────────────────────────────────────────────────────────
async function gql(query: string, variables: Record<string, unknown> = {}, uid = "", signal?: AbortSignal) {
  const res = await fetch("/api/graphql", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-User-ID": uid },
    body: JSON.stringify({ query, variables }),
    signal,
  });
  const json = await res.json();
  if (json.errors) throw new Error(json.errors[0].message);
  return json.data;
}

// ─── Types ─────────────────────────────────────────────────────────────────────
interface Video {
  id: string;
  title: string;
  status: string;
  summary: string | null;
  userId: string;
  isDemo: boolean;
  createdAt: string;
}

interface ChatMsg {
  id: string;
  role: "user" | "assistant";
  content: string;
}

// ─── Status badge ──────────────────────────────────────────────────────────────
function StatusBadge({ status }: { status: string }) {
  const map: Record<string, string> = {
    queued: "bg-neutral-700 text-neutral-300",
    downloading: "bg-blue-900/60 text-blue-300",
    transcribing: "bg-purple-900/60 text-purple-300",
    analyzing_emotions: "bg-pink-900/60 text-pink-300",
    analyzing_visuals: "bg-indigo-900/60 text-indigo-300",
    summarizing: "bg-violet-900/60 text-violet-300",
    completed: "bg-emerald-900/60 text-emerald-300",
    demo: "bg-amber-900/60 text-amber-300",
  };
  const cls = map[status] ?? "bg-neutral-700 text-neutral-400";
  const icon = status === "completed" || status === "demo" ? "✓" : "⏳";
  return (
    <span className={`px-2 py-0.5 rounded-full text-xs font-medium shrink-0 ${cls}`}>
      {icon} {status.replace(/_/g, " ")}
    </span>
  );
}

// ─── Main page ─────────────────────────────────────────────────────────────────
export default function Home() {
  const [uid, setUid] = useState("");
  const [isDragging, setIsDragging] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [videos, setVideos] = useState<Video[]>([]);
  const [videoCount, setVideoCount] = useState(0);

  // Player + chat state
  const [activeVideo, setActiveVideo] = useState<Video | null>(null);
  const [streamUrl, setStreamUrl] = useState<string | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMsg[]>([]);
  const [chatInput, setChatInput] = useState("");
  const [chatLoading, setChatLoading] = useState(false);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const chatEndRef = useRef<HTMLDivElement>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  // Init user ID on mount
  useEffect(() => {
    setUid(getOrCreateUserID());
  }, []);

  // Load video list
  const loadVideos = useCallback(async () => {
    if (!uid) return;
    try {
      const data = await gql(
        `query { videos { id title status summary userId isDemo createdAt } }`,
        {}, uid
      );
      setVideos(data.videos ?? []);
    } catch (e) {
      console.error("Failed to load videos:", e);
    }
  }, [uid]);

  const loadCount = useCallback(async () => {
    if (!uid) return;
    try {
      const data = await gql(`query { userVideoCount }`, {}, uid);
      setVideoCount(data.userVideoCount ?? 0);
    } catch {/* ignore */ }
  }, [uid]);

  useEffect(() => {
    if (uid) {
      loadVideos();
      loadCount();
    }
  }, [uid, loadVideos, loadCount]);

  // ── Server-Sent Events for Status Updates ─────────────────────────────────────
  useEffect(() => {
    if (!uid) return;
    const eventSource = new EventSource(`/api/status-stream?uid=${uid}`);
    eventSource.onmessage = (event) => {
      // Reload videos when we receive a status update
      loadVideos();
    };
    return () => {
      eventSource.close();
    };
  }, [uid, loadVideos]);

  // ── Upload ──────────────────────────────────────────────────────────────────
  const handleFileUpload = async (file: File) => {
    setUploadError(null);
    if (videoCount >= 3) {
      setUploadError("You have reached the 3-video limit. Delete a video to upload a new one.");
      return;
    }
    setUploading(true);
    try {
      const form = new FormData();
      form.append("file", file);
      form.append("title", file.name);
      const res = await fetch("/api/upload", {
        method: "POST",
        headers: { "X-User-ID": uid },
        body: form,
      });
      if (!res.ok) throw new Error((await res.text()) || `Upload failed (${res.status})`);
      await loadVideos();
      await loadCount();
    } catch (e: unknown) {
      setUploadError(e instanceof Error ? e.message : "Upload failed");
    } finally {
      setUploading(false);
    }
  };

  // ── Delete ──────────────────────────────────────────────────────────────────
  const handleDelete = async (id: string) => {
    // if (!confirm("Delete this video and all its chats?")) return;
    try {
      const res = await fetch(`/api/delete?id=${id}`, {
        method: "DELETE",
        headers: { "X-User-ID": uid },
      });
      if (!res.ok && res.status !== 204) throw new Error(await res.text());
      if (activeVideo?.id === id) setActiveVideo(null);
      await loadVideos();
      await loadCount();
    } catch (e: unknown) {
      alert(e instanceof Error ? e.message : "Delete failed");
    }
  };

  // ── Open video player ────────────────────────────────────────────────────────
  const openVideo = async (video: Video) => {
    setActiveVideo(video);
    setMessages([]);
    setSessionId(null);
    setChatInput("");

    // For demo videos that have no real S3 object, skip the stream URL
    if (!video.isDemo) {
      setStreamUrl(`/api/stream?id=${encodeURIComponent(video.id)}`);
    } else {
      setStreamUrl(null);
    }

    // Create a chat session
    try {
      const data = await gql(
        `mutation S($vid: ID!) { createChatSession(videoId: $vid) { id } }`,
        { vid: video.id }, uid
      );
      setSessionId(data.createChatSession.id);
    } catch (e) {
      console.error("Failed to create session:", e);
    }
  };

  // ── Send chat message ────────────────────────────────────────────────────────
  const sendMessage = async () => {
    if (!chatInput.trim() || !sessionId || chatLoading) return;
    const userMsg: ChatMsg = { id: Date.now().toString(), role: "user", content: chatInput };
    setMessages((m) => [...m, userMsg]);
    setChatInput("");
    setChatLoading(true);
    setTimeout(() => chatEndRef.current?.scrollIntoView({ behavior: "smooth" }), 50);

    abortControllerRef.current = new AbortController();
    try {
      const data = await gql(
        `mutation M($sid: ID!, $msg: String!) { sendMessage(sessionId: $sid, message: $msg) { id role content } }`,
        { sid: sessionId, msg: userMsg.content }, uid, abortControllerRef.current.signal
      );
      setMessages((m) => [...m, data.sendMessage]);
    } catch (e: unknown) {
      if (e instanceof Error && e.name === "AbortError") {
        setMessages((m) => [...m, {
          id: Date.now().toString(),
          role: "assistant",
          content: "*Message canceled.*",
        }]);
      } else {
        setMessages((m) => [...m, {
          id: "err",
          role: "assistant",
          content: `Error: ${e instanceof Error ? e.message : "unknown"}`,
        }]);
      }
    } finally {
      setChatLoading(false);
      abortControllerRef.current = null;
      setTimeout(() => chatEndRef.current?.scrollIntoView({ behavior: "smooth" }), 50);
    }
  };

  const cancelMessage = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
  };

  const demoVideos = videos.filter((v) => v.isDemo);
  const myVideos = videos.filter((v) => !v.isDemo);
  const atLimit = videoCount >= 3;

  // ── Render ───────────────────────────────────────────────────────────────────
  return (
    <div className="min-h-screen bg-black text-zinc-100 font-[family-name:var(--font-geist-sans)] relative z-0">
      {/* Diffuse glow background */}
      <div className="absolute top-[-200px] left-1/2 -translate-x-1/2 w-[800px] h-[600px] bg-white/5 blur-[120px] rounded-full pointer-events-none -z-10" />

      {/* Hidden file input */}
      <input
        id="file-upload"
        ref={fileInputRef}
        type="file"
        accept="video/*"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) handleFileUpload(f);
          e.target.value = "";
        }}
      />

      {activeVideo ? (
        /* ═══════════════════ PLAYER VIEW ════════════════════════════════════ */
        <div className="flex flex-col lg:flex-row h-screen">

          {/* Left/Top — Video player */}
          <div className="flex flex-col lg:w-3/5 bg-black">
            {/* Top bar */}
            <div className="flex items-center gap-3 px-4 py-3 bg-zinc-950 border-b border-white/10">
              <button
                onClick={() => setActiveVideo(null)}
                className="p-1.5 rounded-lg text-zinc-400 hover:text-zinc-100 hover:bg-zinc-900 transition-all"
                aria-label="Back to library"
              >
                <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
              <div className="min-w-0">
                <p className="font-semibold text-white truncate text-sm">{activeVideo.title}</p>
                <StatusBadge status={activeVideo.status} />
              </div>
              {!activeVideo.isDemo && (
                <button
                  onClick={() => handleDelete(activeVideo.id)}
                  className="ml-auto shrink-0 px-3 py-1.5 text-xs font-medium bg-red-500/10 border border-red-500/30 text-red-400 hover:bg-red-500/20 rounded-lg transition-all"
                >
                  Delete video
                </button>
              )}
            </div>

            {/* Video element */}
            <div className="flex-1 flex items-center justify-center bg-black">
              {streamUrl ? (
                <video
                  key={activeVideo.id}
                  className="w-full max-h-[calc(100vh-56px)] lg:max-h-[calc(100vh-56px)]"
                  controls
                  preload="metadata"
                  src={streamUrl}
                />
              ) : (
                <div className="text-center text-neutral-600 p-12">
                  <svg className="w-16 h-16 mx-auto mb-4 opacity-40" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M15 10l4.553-2.069A1 1 0 0121 8.868v6.264a1 1 0 01-1.447.894L15 14M3 8a2 2 0 012-2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V8z" />
                  </svg>
                  <p className="text-sm">{activeVideo.isDemo ? "Demo video — no media file available yet." : "Video not available."}</p>
                  {activeVideo.summary && (
                    <p className="mt-3 text-neutral-500 text-sm leading-relaxed max-w-sm mx-auto">{activeVideo.summary}</p>
                  )}
                </div>
              )}
            </div>
          </div>

          {/* Right/Bottom — Chat */}
          <div className="flex flex-col lg:w-2/5 bg-black border-l border-white/10 h-64 lg:h-screen">
            <div className="px-5 py-4 border-b border-white/10 shrink-0">
              <p className="text-sm font-semibold text-zinc-100">Chat with AI about this video</p>
              <p className="text-xs text-zinc-500 mt-0.5">Ask anything — timestamps, people, topics…</p>
            </div>

            {/* Messages */}
            <div className="flex-1 overflow-y-auto px-5 py-4 space-y-3">
              {messages.length === 0 && (
                <div className="text-center py-12 text-neutral-600">
                  <p className="text-sm">No messages yet.</p>
                  <p className="text-xs mt-1">e.g. "What was discussed at 5:30?"</p>
                </div>
              )}
              {messages.map((m) => (
                <div key={m.id} className={`flex ${m.role === "user" ? "justify-end" : "justify-start"}`}>
                  <div className={`max-w-[85%] rounded-2xl px-4 py-2.5 text-sm leading-relaxed break-words ${m.role === "user"
                    ? "bg-zinc-100 text-black rounded-br-sm font-medium"
                    : "bg-zinc-900 text-zinc-300 border border-white/5 rounded-bl-sm"
                    }`}>
                    <ReactMarkdown
                      components={{
                        p: ({ node, ...props }) => <p className="mb-2 last:mb-0" {...props} />,
                        a: ({ node, ...props }) => <a className="underline hover:opacity-80 break-all" target="_blank" rel="noopener noreferrer" {...props} />,
                        code: ({ node, inline, className, children, ...props }: any) => {
                          return !inline ? (
                            <pre className="bg-black/30 p-3 rounded-lg overflow-x-auto my-2 text-xs">
                              <code className={className} {...props}>{children}</code>
                            </pre>
                          ) : (
                            <code className="bg-black/30 rounded px-1.5 py-0.5 text-xs font-mono" {...props}>{children}</code>
                          );
                        },
                        ul: ({ node, ...props }) => <ul className="list-disc ml-4 mb-2" {...props} />,
                        ol: ({ node, ...props }) => <ol className="list-decimal ml-4 mb-2" {...props} />,
                        li: ({ node, ...props }) => <li className="mb-1" {...props} />,
                        h1: ({ node, ...props }) => <h1 className="text-xl font-bold mb-2 mt-4" {...props} />,
                        h2: ({ node, ...props }) => <h2 className="text-lg font-bold mb-2 mt-4" {...props} />,
                        h3: ({ node, ...props }) => <h3 className="text-base font-bold mb-2 mt-3" {...props} />,
                      }}
                    >
                      {m.content}
                    </ReactMarkdown>
                  </div>
                </div>
              ))}
              {chatLoading && (
                <div className="flex justify-start">
                  <div className="bg-zinc-900 border border-white/5 rounded-2xl rounded-bl-sm px-4 py-3 flex gap-1.5">
                    {[0, 150, 300].map((d) => (
                      <span key={d} className="w-2 h-2 bg-zinc-500 rounded-full animate-bounce" style={{ animationDelay: `${d}ms` }} />
                    ))}
                  </div>
                </div>
              )}
              <div ref={chatEndRef} />
            </div>

            {/* Input */}
            <div className="px-5 py-4 border-t border-white/10 shrink-0">
              {activeVideo.status !== "completed" && !activeVideo.isDemo ? (
                <p className="text-xs text-amber-400 text-center">Analysis in progress — chat available once complete.</p>
              ) : (
                <div className="flex gap-2">
                  <input
                    id="chat-input"
                    type="text"
                    value={chatInput}
                    onChange={(e) => setChatInput(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && !e.shiftKey && sendMessage()}
                    placeholder="Ask about this video…"
                    disabled={chatLoading || !sessionId}
                    className="flex-1 bg-zinc-950 border border-white/10 rounded-xl px-4 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none focus:ring-1 focus:ring-zinc-400 focus:border-zinc-400 disabled:opacity-50 transition-all"
                  />
                  {chatLoading ? (
                    <button
                      onClick={cancelMessage}
                      className="px-4 py-2.5 bg-zinc-900 hover:bg-zinc-800 border border-white/10 text-zinc-100 rounded-xl transition-all flex items-center justify-center shrink-0"
                      title="Cancel generation"
                    >
                      <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                        <rect x="6" y="6" width="12" height="12" rx="2" />
                      </svg>
                    </button>
                  ) : (
                    <button
                      id="send-message"
                      onClick={sendMessage}
                      disabled={!chatInput.trim() || !sessionId}
                      className="px-4 py-2.5 bg-zinc-100 hover:bg-white disabled:opacity-40 disabled:cursor-not-allowed text-black font-medium rounded-xl transition-all flex items-center justify-center shrink-0"
                      title="Send message"
                    >
                      <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
                      </svg>
                    </button>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>

      ) : (
        /* ═══════════════════ LIBRARY VIEW ═══════════════════════════════════ */
        <main className="max-w-5xl mx-auto px-6 py-16 flex flex-col gap-14">

          {/* Header */}
          <div className="text-center space-y-4">
            <h1 className="text-5xl md:text-7xl font-bold tracking-tight text-neutral-100">
              Video Summarization Tool
            </h1>
            <p className="text-lg text-neutral-400 max-w-2xl mx-auto leading-relaxed">
              Upload a video and ask the AI anything about it. It watches the whole video and remembers every detail.
            </p>
          </div>

          {/* Upload section */}
          <div className="w-full max-w-2xl mx-auto bg-zinc-950/50 backdrop-blur-xl border border-white/10 rounded-3xl p-7 shadow-2xl relative">
            <div className="flex items-center justify-between mb-5">
              <h2 className="text-base font-semibold text-zinc-100">Upload a Video</h2>
              <span className={`text-sm font-medium px-3 py-1 rounded-full border ${atLimit
                ? "text-red-400 bg-red-500/10 border-red-500/30"
                : "text-zinc-400 bg-zinc-900 border-white/10"
                }`}>
                {videoCount}/3 used
              </span>
            </div>

            <div
              id="drop-zone"
              role="button"
              tabIndex={0}
              aria-label="Click to select a video or drag and drop"
              onClick={() => !uploading && !atLimit && fileInputRef.current?.click()}
              onKeyDown={(e) => e.key === "Enter" && !atLimit && fileInputRef.current?.click()}
              onDragOver={(e) => { e.preventDefault(); if (!atLimit) setIsDragging(true); }}
              onDragLeave={() => setIsDragging(false)}
              onDrop={(e) => {
                e.preventDefault();
                setIsDragging(false);
                const f = e.dataTransfer.files[0];
                if (f) handleFileUpload(f);
              }}
              className={`
                min-h-[160px] flex items-center justify-center
                border border-dashed rounded-2xl transition-all
                ${atLimit
                  ? "border-white/5 bg-zinc-900/20 opacity-50 cursor-not-allowed"
                  : isDragging
                    ? "border-zinc-400 bg-zinc-800/50 scale-[1.01] cursor-pointer"
                    : "border-white/10 bg-black/40 hover:bg-black/60 hover:border-white/30 cursor-pointer"
                }
                ${uploading ? "opacity-60 cursor-not-allowed" : ""}
              `}
            >
              <div className="text-center space-y-3 py-8 pointer-events-none">
                <div className={`w-14 h-14 rounded-full flex items-center justify-center mx-auto transition-all border border-white/5 ${isDragging ? "bg-zinc-800 text-zinc-200 scale-110" : "bg-zinc-900 text-zinc-400"
                  }`}>
                  {uploading ? (
                    <svg className="w-7 h-7 animate-spin" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                    </svg>
                  ) : (
                    <svg className="w-7 h-7" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
                    </svg>
                  )}
                </div>
                <div>
                  <p className="text-sm font-medium text-white">
                    {atLimit ? "Upload limit reached" : uploading ? "Uploading…" : isDragging ? "Drop to upload" : "Click to select or drag & drop"}
                  </p>
                  <p className="text-xs text-zinc-500 mt-1">MP4, WebM or MOV · max 2 GB</p>
                </div>
              </div>
            </div>

            {uploadError && (
              <p className="mt-3 text-sm text-red-400 bg-red-500/10 rounded-xl px-4 py-2">{uploadError}</p>
            )}
          </div>

          {/* My Videos */}
          <section className="w-full max-w-4xl mx-auto space-y-4">
            <div className="flex items-center justify-between">
              <h2 className="text-xl font-semibold text-zinc-100">My Videos</h2>
              <button
                id="refresh-videos"
                onClick={() => { loadVideos(); loadCount(); }}
                className="text-sm text-zinc-500 hover:text-zinc-100 transition-colors flex items-center gap-1.5"
              >
                <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
                Refresh
              </button>
            </div>

            {myVideos.length === 0 ? (
              <div className="text-center py-12 text-zinc-600 border border-white/5 rounded-2xl">
                <p className="text-sm">No videos yet. Upload one above.</p>
              </div>
            ) : (
              <div className="grid gap-3">
                {myVideos.map((v) => (
                  <VideoCard
                    key={v.id}
                    video={v}
                    onOpen={() => openVideo(v)}
                    onDelete={() => handleDelete(v.id)}
                    showDelete
                  />
                ))}
              </div>
            )}
          </section>

          {/* Demo Videos */}
          <section className="w-full max-w-4xl mx-auto space-y-4">
            <div>
              <h2 className="text-xl font-semibold text-zinc-100">Demo Videos</h2>
              <p className="text-sm text-zinc-500 mt-0.5">Sample videos available to everyone for exploration.</p>
            </div>
            <div className="grid gap-3">
              {demoVideos.map((v) => (
                <VideoCard
                  key={v.id}
                  video={v}
                  onOpen={() => openVideo(v)}
                  showDelete={false}
                />
              ))}
            </div>
          </section>
        </main>
      )}
    </div>
  );
}

// ─── VideoCard component ───────────────────────────────────────────────────────
function VideoCard({
  video,
  onOpen,
  onDelete,
  showDelete,
}: {
  video: Video;
  onOpen: () => void;
  onDelete?: () => void;
  showDelete: boolean;
}) {
  return (
    <div className="flex items-center gap-4 bg-zinc-950/50 border border-white/5 hover:border-white/10 rounded-2xl p-4 transition-all group">
      {/* Thumbnail placeholder */}
      <div
        className="shrink-0 w-20 h-14 bg-zinc-900 border border-white/5 rounded-xl flex items-center justify-center cursor-pointer hover:bg-zinc-800 transition-all"
        onClick={onOpen}
      >
        <svg className="w-6 h-6 text-zinc-600 group-hover:text-zinc-300 transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
      </div>

      {/* Info */}
      <div className="flex-1 min-w-0 cursor-pointer" onClick={onOpen}>
        <div className="flex items-center gap-2 flex-wrap">
          <p className="font-medium text-zinc-100 text-sm truncate">{video.title}</p>
          <StatusBadge status={video.isDemo ? "demo" : video.status} />
        </div>
        {video.summary && (
          <p className="text-xs text-zinc-500 mt-1 line-clamp-1">{video.summary}</p>
        )}
        <p className="text-xs text-zinc-700 mt-1">{new Date(video.createdAt).toLocaleDateString()}</p>
      </div>

      {/* Actions */}
      <div className="shrink-0 flex gap-2">
        <button
          onClick={onOpen}
          className="px-3 py-1.5 bg-zinc-900 border border-white/10 hover:bg-zinc-800 text-zinc-300 hover:text-white text-xs font-medium rounded-xl transition-all"
        >
          {video.status === "completed" || video.isDemo ? "Watch & Chat" : "View"}
        </button>
        {showDelete && onDelete && (
          <button
            onClick={(e) => { e.stopPropagation(); onDelete(); }}
            className="p-1.5 text-zinc-600 hover:text-red-400 hover:bg-red-500/10 rounded-xl transition-all"
            aria-label="Delete video"
          >
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
          </button>
        )}
      </div>
    </div>
  );
}
