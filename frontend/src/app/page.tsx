"use client";

import { useState } from "react";

export default function Home() {
  const [activeTab, setActiveTab] = useState<"upload" | "youtube">("upload");
  const [searchQuery, setSearchQuery] = useState("");

  return (
    <div className="min-h-screen bg-neutral-950 text-white font-[family-name:var(--font-geist-sans)] selection:bg-indigo-500/30">
      <main className="max-w-5xl mx-auto px-6 py-24 flex flex-col items-center gap-16">
        
        {/* Header Section */}
        <div className="text-center space-y-6 max-w-3xl">
          <h1 className="text-5xl md:text-7xl font-bold tracking-tight bg-clip-text text-transparent bg-gradient-to-r from-indigo-400 via-purple-400 to-pink-400">
            Semantic Video Search
          </h1>
          <p className="text-lg md:text-xl text-neutral-400 leading-relaxed">
            Upload your meeting recordings or provide a YouTube link. Our pipeline extracts audio, transcribes, embeds, and enables natural language search across your entire video history.
          </p>
        </div>

        {/* Action Panel */}
        <div className="w-full max-w-2xl bg-neutral-900/50 backdrop-blur-xl border border-neutral-800 rounded-3xl p-8 shadow-2xl">
          {/* Tabs */}
          <div className="flex bg-neutral-800/50 rounded-xl p-1 mb-8">
            <button
              onClick={() => setActiveTab("upload")}
              className={`flex-1 py-3 px-4 text-sm font-medium rounded-lg transition-all duration-200 ${
                activeTab === "upload" 
                  ? "bg-indigo-500 text-white shadow-lg shadow-indigo-500/20" 
                  : "text-neutral-400 hover:text-white hover:bg-neutral-800"
              }`}
            >
              Upload Video
            </button>
            <button
              onClick={() => setActiveTab("youtube")}
              className={`flex-1 py-3 px-4 text-sm font-medium rounded-lg transition-all duration-200 ${
                activeTab === "youtube" 
                  ? "bg-indigo-500 text-white shadow-lg shadow-indigo-500/20" 
                  : "text-neutral-400 hover:text-white hover:bg-neutral-800"
              }`}
            >
              YouTube Link
            </button>
          </div>

          {/* Tab Content */}
          <div className="min-h-[200px] flex items-center justify-center border-2 border-dashed border-neutral-700/50 rounded-2xl bg-neutral-900/20 hover:bg-neutral-900/40 hover:border-indigo-500/50 transition-all group">
            {activeTab === "upload" ? (
              <div className="text-center space-y-4 py-12">
                <div className="w-16 h-16 bg-indigo-500/10 text-indigo-400 rounded-full flex items-center justify-center mx-auto group-hover:scale-110 group-hover:bg-indigo-500/20 transition-all">
                  <svg className="w-8 h-8" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
                  </svg>
                </div>
                <div>
                  <p className="text-base font-medium text-white">Click to upload or drag and drop</p>
                  <p className="text-sm text-neutral-500 mt-1">MP4, WebM or OGG (max. 2GB)</p>
                </div>
              </div>
            ) : (
              <div className="w-full px-8 py-12 space-y-4">
                <label className="block text-sm font-medium text-neutral-300">YouTube URL</label>
                <div className="flex gap-4">
                  <input 
                    type="url" 
                    placeholder="https://youtube.com/watch?v=..."
                    className="flex-1 bg-neutral-950 border border-neutral-800 rounded-xl px-4 py-3 text-white placeholder-neutral-600 focus:outline-none focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 transition-all"
                  />
                  <button className="px-6 py-3 bg-indigo-500 hover:bg-indigo-400 text-white font-medium rounded-xl transition-all shadow-lg shadow-indigo-500/20">
                    Process
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Semantic Search Section */}
        <div className="w-full max-w-4xl space-y-8 mt-12">
          <div className="text-center space-y-2">
            <h2 className="text-3xl font-semibold text-white">Search History</h2>
            <p className="text-neutral-400">Ask a question to find the exact moment it was discussed.</p>
          </div>
          
          <div className="relative group">
            <div className="absolute inset-0 bg-gradient-to-r from-indigo-500 to-purple-500 rounded-2xl blur opacity-20 group-hover:opacity-40 transition duration-500"></div>
            <div className="relative flex items-center bg-neutral-900 border border-neutral-800 rounded-2xl p-2 shadow-2xl">
              <div className="pl-4 pr-2 text-neutral-500">
                <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
                </svg>
              </div>
              <input 
                type="text" 
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="e.g., 'Find the moment where we discussed the new pricing architecture'"
                className="flex-1 bg-transparent border-none text-white px-2 py-4 focus:outline-none focus:ring-0 placeholder-neutral-600 text-lg"
              />
              <button className="px-8 py-4 bg-white hover:bg-neutral-200 text-black font-semibold rounded-xl transition-all shadow-lg">
                Search
              </button>
            </div>
          </div>
        </div>

      </main>
    </div>
  );
}
