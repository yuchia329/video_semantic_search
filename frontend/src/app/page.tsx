"use client";
import { useState, useEffect, useRef } from "react";

// Helper for GraphQL calls
async function fetchGraphQL(query: string, variables: any = {}) {
  const res = await fetch("http://localhost:8080/query", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query, variables }),
  });
  if (!res.ok) throw new Error("GraphQL request failed");
  const data = await res.json();
  if (data.errors) throw new Error(data.errors[0].message);
  return data.data;
}

export default function Home() {
  const [activeTab, setActiveTab] = useState<"upload" | "youtube">("upload");
  const [youtubeUrl, setYoutubeUrl] = useState("");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  
  const [status, setStatus] = useState("");
  const [loading, setLoading] = useState(false);
  const [videos, setVideos] = useState<any[]>([]);

  // Chat UI state
  const [selectedSearchVideoId, setSelectedSearchVideoId] = useState<string | null>(null);
  const [chatMessage, setChatMessage] = useState("");
  const [chatHistory, setChatHistory] = useState<{role: string, content: string, thinking?: string, references?: any[]}[]>([]);
  const chatScrollRef = useRef<HTMLDivElement>(null);

  const limitReached = videos.length >= 2;

  // Fetch recent videos to show status
  const fetchVideos = async () => {
    try {
      const data = await fetchGraphQL(`
        query {
          getAllVideos {
            id title status summary createdAt
          }
        }
      `);
      setVideos(data.getAllVideos);
    } catch (e) {
      console.error(e);
    }
  };

  useEffect(() => {
    fetchVideos();
    const interval = setInterval(fetchVideos, 5000); // Poll status every 5s
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    // scroll to bottom of chat
    if (chatScrollRef.current) {
      chatScrollRef.current.scrollTop = chatScrollRef.current.scrollHeight;
    }
  }, [chatHistory]);

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) {
      setSelectedFile(e.target.files[0]);
    }
  };

  const handleFileUpload = async () => {
    if (!selectedFile) return;
    if (limitReached) {
      setStatus("Limit reached. Please delete a video first.");
      return;
    }
    
    try {
      setLoading(true);
      setStatus("Requesting upload URL...");
      
      const reqRes = await fetchGraphQL(`
        mutation($input: UploadVideoInput!) {
          requestVideoUpload(input: $input) { uploadUrl videoId s3Key }
        }
      `, { input: { title: selectedFile.name, filename: selectedFile.name, contentType: selectedFile.type } });
      
      const { uploadUrl, videoId } = reqRes.requestVideoUpload;

      setStatus("Uploading to storage...");
      await fetch(uploadUrl, { method: "PUT", body: selectedFile, headers: { "Content-Type": selectedFile.type } });

      setStatus("Finalizing upload...");
      await fetchGraphQL(`
        mutation($input: FinalizeVideoInput!) {
          finalizeVideoUpload(input: $input) { id }
        }
      `, { input: { videoId } });

      setStatus("Upload complete! Processing will begin.");
      setSelectedFile(null);
      fetchVideos();
    } catch (err: any) {
      setStatus(`Error: ${err.message}`);
    } finally {
      setLoading(false);
    }
  };

  const handleYoutubeSubmit = async () => {
    if (!youtubeUrl) return;
    if (limitReached) {
      setStatus("Limit reached. Please delete a video first.");
      return;
    }
    try {
      setLoading(true);
      setStatus("Submitting YouTube link...");
      await fetchGraphQL(`
        mutation($input: YouTubeInput!) {
          processYouTubeVideo(input: $input) { id }
        }
      `, { input: { url: youtubeUrl } });
      setStatus("Link submitted! Processing will begin.");
      setYoutubeUrl("");
      fetchVideos();
    } catch (err: any) {
      setStatus(`Error: ${err.message}`);
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteVideo = async (videoId: string) => {
    try {
      setLoading(true);
      await fetchGraphQL(`
        mutation($id: ID!) {
          deleteVideo(videoId: $id)
        }
      `, { id: videoId });
      if (selectedSearchVideoId === videoId) {
        setSelectedSearchVideoId(null);
        setChatHistory([]);
      }
      fetchVideos();
    } catch (err: any) {
      setStatus(`Delete Error: ${err.message}`);
    } finally {
      setLoading(false);
    }
  }

  const handleChat = async () => {
    if (!chatMessage || !selectedSearchVideoId) return;
    
    const userMessage = chatMessage;
    setChatMessage("");
    setChatHistory(prev => [...prev, { role: "user", content: userMessage }]);
    
    try {
      setLoading(true);
      const historyStrings = chatHistory.map(m => m.content);
      
      const data = await fetchGraphQL(`
        query($videoId: ID!, $message: String!, $history: [String!]) {
          chatWithVideo(videoId: $videoId, message: $message, history: $history) {
            answer
            thinking
            references {
              timestamp text type videoTitle
            }
          }
        }
      `, { videoId: selectedSearchVideoId, message: userMessage, history: historyStrings });
      
      const res = data.chatWithVideo;
      setChatHistory(prev => [...prev, { role: "assistant", content: res.answer, thinking: res.thinking || undefined, references: res.references }]);
    } catch (err: any) {
      console.error(err);
      setChatHistory(prev => [...prev, { role: "assistant", content: `Error: ${err.message}` }]);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-neutral-950 text-white font-[family-name:var(--font-geist-sans)] selection:bg-indigo-500/30">
      <main className="max-w-5xl mx-auto px-6 py-24 flex flex-col items-center gap-16">
        {/* Header Section */}
        <div className="text-center space-y-6 max-w-3xl">
          <h1 className="text-5xl md:text-7xl font-bold tracking-tight bg-clip-text text-transparent bg-gradient-to-r from-indigo-400 via-purple-400 to-pink-400">
            Semantic Video Search
          </h1>
          <p className="text-lg md:text-xl text-neutral-400 leading-relaxed">
            Upload your meeting recordings or provide a YouTube link. Our pipeline extracts audio, transcribes, embeds, and enables natural language search and AI chat across your video.
          </p>
        </div>

        {/* Action Panel */}
        <div className="w-full max-w-2xl bg-neutral-900/50 backdrop-blur-xl border border-neutral-800 rounded-3xl p-8 shadow-2xl">
          {limitReached && (
            <div className="mb-6 p-4 bg-red-500/10 border border-red-500/20 rounded-xl text-red-400 text-center font-medium">
              You have reached the maximum limit of 2 videos. Please delete an existing video to upload a new one.
            </div>
          )}

          <div className="flex bg-neutral-800/50 rounded-xl p-1 mb-8">
            <button onClick={() => setActiveTab("upload")} className={`flex-1 py-3 px-4 text-sm font-medium rounded-lg transition-all duration-200 ${activeTab === "upload" ? "bg-indigo-500 text-white shadow-lg shadow-indigo-500/20" : "text-neutral-400 hover:text-white hover:bg-neutral-800"}`}>
              Upload Video
            </button>
            <button onClick={() => setActiveTab("youtube")} className={`flex-1 py-3 px-4 text-sm font-medium rounded-lg transition-all duration-200 ${activeTab === "youtube" ? "bg-indigo-500 text-white shadow-lg shadow-indigo-500/20" : "text-neutral-400 hover:text-white hover:bg-neutral-800"}`}>
              YouTube Link
            </button>
          </div>

          <div className="min-h-[200px] flex items-center justify-center border-2 border-dashed border-neutral-700/50 rounded-2xl bg-neutral-900/20 hover:bg-neutral-900/40 hover:border-indigo-500/50 transition-all group">
            {activeTab === "upload" ? (
              <div className="w-full h-full py-12 flex flex-col items-center justify-center">
                {!selectedFile ? (
                   <label className={`w-full h-full text-center space-y-4 cursor-pointer flex flex-col items-center justify-center ${limitReached ? 'opacity-50 pointer-events-none' : ''}`}>
                     <input type="file" className="hidden" accept="video/mp4,video/webm,video/quicktime" onChange={handleFileSelect} disabled={loading || limitReached} />
                     <div className="w-16 h-16 bg-indigo-500/10 text-indigo-400 rounded-full flex items-center justify-center mx-auto group-hover:scale-110 group-hover:bg-indigo-500/20 transition-all">
                       <svg className="w-8 h-8" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                         <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
                       </svg>
                     </div>
                     <div>
                       <p className="text-base font-medium text-white">Click to select or drag and drop</p>
                       <p className="text-sm text-neutral-500 mt-1">MP4, WebM, or MOV</p>
                     </div>
                   </label>
                ) : (
                  <div className="text-center space-y-6 w-full px-8">
                     <div className="p-4 bg-neutral-800 rounded-xl border border-neutral-700">
                        <p className="text-white font-medium truncate">{selectedFile.name}</p>
                        <p className="text-neutral-400 text-sm mt-1">{(selectedFile.size / (1024*1024)).toFixed(2)} MB</p>
                     </div>
                     <div className="flex gap-4 justify-center">
                       <button onClick={() => setSelectedFile(null)} disabled={loading} className="px-6 py-3 bg-neutral-800 hover:bg-neutral-700 text-white font-medium rounded-xl transition-all">
                         Cancel
                       </button>
                       <button onClick={handleFileUpload} disabled={loading || limitReached} className="px-8 py-3 bg-indigo-500 hover:bg-indigo-400 text-white font-medium rounded-xl transition-all shadow-lg shadow-indigo-500/20 disabled:opacity-50">
                         {loading ? "Processing..." : "Process Video"}
                       </button>
                     </div>
                  </div>
                )}
              </div>
            ) : (
              <div className="w-full px-8 py-12 space-y-4">
                <label className="block text-sm font-medium text-neutral-300">YouTube URL</label>
                <div className="flex gap-4">
                  <input type="url" value={youtubeUrl} onChange={e => setYoutubeUrl(e.target.value)} placeholder="https://youtube.com/watch?v=..." className="flex-1 bg-neutral-950 border border-neutral-800 rounded-xl px-4 py-3 text-white placeholder-neutral-600 focus:outline-none focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 transition-all" disabled={limitReached}/>
                  <button onClick={handleYoutubeSubmit} disabled={loading || limitReached} className="px-6 py-3 bg-indigo-500 hover:bg-indigo-400 text-white font-medium rounded-xl transition-all shadow-lg shadow-indigo-500/20 disabled:opacity-50">
                    Process
                  </button>
                </div>
              </div>
            )}
          </div>
          {status && <div className="mt-4 text-center text-sm text-indigo-300 font-medium animate-pulse">{status}</div>}
        </div>

        {/* Video Status List */}
        {videos.length > 0 && (
          <div className="w-full max-w-4xl space-y-4">
             <div className="flex justify-between items-center">
               <h3 className="text-xl font-semibold text-white">Your Videos ({videos.length}/2)</h3>
             </div>
             <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
               {videos.map(v => (
                 <div key={v.id} className={`bg-neutral-900 border rounded-xl p-5 flex flex-col gap-4 transition-all ${selectedSearchVideoId === v.id ? 'border-indigo-500 ring-1 ring-indigo-500 shadow-lg shadow-indigo-500/10' : 'border-neutral-800 hover:border-neutral-700'}`}>
                   <div className="flex justify-between items-start gap-4">
                     <span className="font-medium text-white truncate flex-1" title={v.title}>{v.title}</span>
                     <span className={`text-xs px-2.5 py-1 rounded-full whitespace-nowrap ${v.status === 'completed' ? 'bg-green-500/10 text-green-400 border border-green-500/20' : 'bg-yellow-500/10 text-yellow-400 border border-yellow-500/20 animate-pulse'}`}>
                       {v.status}
                     </span>
                   </div>
                   
                   <div className="flex gap-3 mt-auto pt-2">
                     <button 
                       onClick={() => {
                         setSelectedSearchVideoId(v.id);
                         setChatHistory([]);
                       }}
                       disabled={v.status !== 'completed'}
                       className={`flex-1 py-2 px-3 text-sm font-medium rounded-lg transition-all ${selectedSearchVideoId === v.id ? 'bg-indigo-500 text-white' : 'bg-neutral-800 text-neutral-300 hover:bg-neutral-700 disabled:opacity-50 disabled:hover:bg-neutral-800'}`}
                     >
                       {selectedSearchVideoId === v.id ? 'Chat Active' : 'Talk to AI'}
                     </button>
                     <button 
                       onClick={() => handleDeleteVideo(v.id)}
                       disabled={loading}
                       className="py-2 px-3 text-sm font-medium bg-red-500/10 text-red-400 hover:bg-red-500/20 rounded-lg transition-all"
                     >
                       Delete
                     </button>
                   </div>
                 </div>
               ))}
             </div>
          </div>
        )}

        {/* Talk to AI Section */}
        {selectedSearchVideoId && (
          <div className="w-full max-w-4xl space-y-6 mt-8">
            <div className="text-center space-y-2">
              <h2 className="text-3xl font-semibold text-white">Talk to AI</h2>
              <p className="text-neutral-400">Have a conversation about the selected video.</p>
            </div>

            <div className="bg-neutral-900 border border-neutral-800 rounded-3xl overflow-hidden flex flex-col shadow-2xl h-[600px]">
              
              {/* Chat History Area */}
              <div ref={chatScrollRef} className="flex-1 min-h-0 overflow-y-auto p-6 space-y-6 scroll-smooth">
                {chatHistory.length === 0 ? (
                  <div className="h-full flex items-center justify-center text-neutral-500">
                    <p>Ask a question like "What is this video about?" or "When does the user sound excited?"</p>
                  </div>
                ) : (
                  chatHistory.map((msg, i) => (
                    <div key={i} className={`flex flex-col max-w-[85%] ${msg.role === 'user' ? 'ml-auto items-end' : 'mr-auto items-start'}`}>
                      {/* Thinking process (collapsed by default) */}
                      {msg.role === 'assistant' && msg.thinking && (
                        <details className="mb-2 w-full">
                          <summary className="cursor-pointer text-xs font-medium text-purple-400 hover:text-purple-300 transition-colors flex items-center gap-1.5 select-none py-1 px-2 rounded-lg hover:bg-purple-500/10">
                            <svg className="w-3.5 h-3.5 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
                            </svg>
                            Thinking process
                          </summary>
                          <div className="mt-1.5 px-4 py-3 bg-purple-500/5 border border-purple-500/15 rounded-xl text-xs text-neutral-400 whitespace-pre-wrap leading-relaxed max-h-60 overflow-y-auto">
                            {msg.thinking}
                          </div>
                        </details>
                      )}

                      {/* Main message bubble */}
                      <div className={`px-5 py-3.5 rounded-2xl ${msg.role === 'user' ? 'bg-indigo-600 text-white rounded-br-sm' : 'bg-neutral-800 text-neutral-200 border border-neutral-700 rounded-bl-sm'}`}>
                        <p className="whitespace-pre-wrap">{msg.content}</p>
                      </div>
                      
                      {/* Source References (collapsed by default) */}
                      {msg.references && msg.references.length > 0 && (
                        <details className="mt-2 w-full max-w-sm">
                          <summary className="cursor-pointer text-xs font-medium text-neutral-500 hover:text-neutral-400 transition-colors flex items-center gap-1.5 select-none py-1 px-1">
                            <svg className="w-3.5 h-3.5 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
                            </svg>
                            {msg.references.length} source{msg.references.length !== 1 ? 's' : ''} referenced
                          </summary>
                          <div className="mt-1.5 space-y-2">
                            {msg.references.map((ref: any, j: number) => (
                              <div key={j} className="bg-neutral-800/50 border border-neutral-700/50 rounded-lg p-2.5 text-xs flex gap-3 items-start">
                                <span className="bg-neutral-700 text-neutral-300 font-mono px-1.5 py-0.5 rounded flex-shrink-0">
                                  {Math.floor(ref.timestamp / 60)}:{(ref.timestamp % 60).toString().padStart(2, '0')}
                                </span>
                                <p className="text-neutral-400 line-clamp-2 italic">"{ref.text}"</p>
                              </div>
                            ))}
                          </div>
                        </details>
                      )}
                    </div>
                  ))
                )}
                {loading && (
                   <div className="flex flex-col max-w-[85%] mr-auto items-start">
                     <div className="px-5 py-3.5 rounded-2xl bg-neutral-800 text-neutral-200 border border-neutral-700 rounded-bl-sm flex gap-2 items-center">
                       <div className="w-2 h-2 bg-indigo-500 rounded-full animate-bounce"></div>
                       <div className="w-2 h-2 bg-purple-500 rounded-full animate-bounce delay-75"></div>
                       <div className="w-2 h-2 bg-pink-500 rounded-full animate-bounce delay-150"></div>
                     </div>
                   </div>
                )}
              </div>

              {/* Chat Input Area */}
              <div className="p-4 bg-neutral-950 border-t border-neutral-800">
                <div className="relative flex items-center bg-neutral-900 border border-neutral-700 hover:border-neutral-600 focus-within:border-indigo-500 focus-within:ring-1 focus-within:ring-indigo-500 rounded-2xl transition-all p-1">
                  <input
                    type="text"
                    value={chatMessage}
                    onChange={(e) => setChatMessage(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && !loading && handleChat()}
                    placeholder="Ask something about this video..."
                    className="flex-1 bg-transparent border-none text-white px-4 py-3 focus:outline-none focus:ring-0 placeholder-neutral-500"
                    disabled={loading}
                  />
                  <button 
                    onClick={handleChat} 
                    disabled={loading || !chatMessage.trim()} 
                    className="p-3 bg-indigo-600 hover:bg-indigo-500 text-white rounded-xl transition-all disabled:opacity-50 disabled:hover:bg-indigo-600 mr-1"
                  >
                    <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M12 5l7 7-7 7" />
                    </svg>
                  </button>
                </div>
              </div>

            </div>
          </div>
        )}

      </main>
    </div>
  );
}