package worker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuchia329/video_semantic_search/backend/internal/kafka"
	"github.com/yuchia329/video_semantic_search/backend/internal/storage"
)

const (
	transcriptionURL = "http://localhost:8902/transcribe"
	emotionURL       = "http://localhost:8904/emotion"
	vllmURL          = "http://localhost:8900/v1/chat/completions"
	vllmModel        = "Qwen/Qwen2.5-VL-7B-Instruct"

	// 0.5 fps = 1 frame every 2 seconds.
	// For a 10-min video this yields 300 frames — a safe balance between
	// visual coverage and VRAM/payload size.
	framesPerSecond = "1/2"
)

// ─── Processor ──────────────────────────────────────────────────────────────

type Processor struct {
	consumer *kafka.Consumer
	db       *storage.DB
	s3       *storage.S3Client
}

type ProcessVideoEvent struct {
	VideoID string `json:"video_id"`
	S3Key   string `json:"s3_key"`
}

func NewProcessor(consumer *kafka.Consumer, db *storage.DB, s3 *storage.S3Client) *Processor {
	return &Processor{consumer: consumer, db: db, s3: s3}
}

func (p *Processor) Start(ctx context.Context) {
	log.Println("Starting Video Processor worker...")
	p.consumer.Start(ctx, p.handleMessage)
}

// ─── Pipeline ────────────────────────────────────────────────────────────────

func (p *Processor) handleMessage(ctx context.Context, key, value []byte) error {
	var event ProcessVideoEvent
	if err := json.Unmarshal(value, &event); err != nil {
		return err
	}
	log.Printf("[%s] Processing started (S3 key: %s)", event.VideoID, event.S3Key)

	// ── Temp workspace ──────────────────────────────────────────────────────
	tmpDir, err := os.MkdirTemp("", "video_process_*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "video.mp4")
	audioPath := filepath.Join(tmpDir, "audio.wav")
	framesDir := filepath.Join(tmpDir, "frames")
	_ = os.Mkdir(framesDir, 0o755)

	// ── 1. Download ─────────────────────────────────────────────────────────
	setStatus(ctx, p.db, event.VideoID, "downloading")
	if strings.HasPrefix(event.S3Key, "http") {
		cmd := exec.Command("yt-dlp", "-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/mp4",
			"-o", videoPath, event.S3Key)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("yt-dlp failed: %w — %s", err, out)
		}
	} else {
		f, err := os.Create(videoPath)
		if err != nil {
			return fmt.Errorf("create video file: %w", err)
		}
		if err = p.s3.DownloadFile(ctx, event.S3Key, f); err != nil {
			f.Close()
			return fmt.Errorf("S3 download: %w", err)
		}
		f.Close()
	}

	// ── 2. Extract audio (16 kHz mono WAV for WhisperX + emotion model) ─────
	setStatus(ctx, p.db, event.VideoID, "extracting")
	run("ffmpeg", "-y", "-i", videoPath,
		"-vn", "-acodec", "pcm_s16le", "-ar", "16000", "-ac", "1", audioPath)

	// ── 3. Extract frames at 0.5 fps ────────────────────────────────────────
	run("ffmpeg", "-y", "-i", videoPath,
		"-vf", fmt.Sprintf("fps=%s,scale=854:480", framesPerSecond),
		filepath.Join(framesDir, "frame_%06d.jpg"))

	// ── 4. Transcribe ───────────────────────────────────────────────────────
	setStatus(ctx, p.db, event.VideoID, "transcribing")
	transcript, err := transcribeAudio(audioPath)
	if err != nil {
		log.Printf("[%s] Transcription error: %v", event.VideoID, err)
		transcript = &TranscriptionResponse{}
	}

	// ── 5. Emotion classification ────────────────────────────────────────────
	setStatus(ctx, p.db, event.VideoID, "analyzing_emotions")
	emotions, err := classifyEmotions(audioPath, transcript.Segments)
	if err != nil {
		log.Printf("[%s] Emotion error: %v", event.VideoID, err)
		emotions = nil
	}

	// ── 6. Build VLM context and run analysis ────────────────────────────────
	setStatus(ctx, p.db, event.VideoID, "analyzing_visuals")
	framePaths := listSortedFiles(framesDir)
	analysis, err := analyzeVideoWithVLM(framePaths, transcript, emotions)
	if err != nil {
		log.Printf("[%s] VLM analysis error: %v", event.VideoID, err)
		analysis = "Video analysis could not be completed."
	}

	// ── 7. Derive a short summary from the analysis ──────────────────────────
	setStatus(ctx, p.db, event.VideoID, "summarizing")
	summary := extractSummaryFromAnalysis(analysis)

	// ── 8. Persist ───────────────────────────────────────────────────────────
	_, _ = p.db.Pool.Exec(ctx,
		`UPDATE videos SET status='completed', summary=$1, analysis=$2 WHERE id=$3`,
		summary, analysis, event.VideoID)

	log.Printf("[%s] Processing complete", event.VideoID)
	return nil
}

// ─── Step 4: Transcription ───────────────────────────────────────────────────

type TranscriptionSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type TranscriptionResponse struct {
	Language string                 `json:"language"`
	Segments []TranscriptionSegment `json:"segments"`
}

func transcribeAudio(filePath string) (*TranscriptionResponse, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", filepath.Base(filePath))
	io.Copy(part, f)
	w.Close()

	resp, err := http.Post(transcriptionURL, w.FormDataContentType(), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res TranscriptionResponse
	json.NewDecoder(resp.Body).Decode(&res)
	return &res, nil
}

// ─── Step 5: Emotion classification ─────────────────────────────────────────

type EmotionSegment struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Text       string  `json:"text"`
	Emotion    string  `json:"emotion"`
	Confidence float64 `json:"confidence"`
}

type EmotionResponse struct {
	Segments []EmotionSegment `json:"segments"`
}

func classifyEmotions(audioPath string, segs []TranscriptionSegment) ([]EmotionSegment, error) {
	if len(segs) == 0 {
		return nil, nil
	}

	segJSON, _ := json.Marshal(segs)

	f, err := os.Open(audioPath)
	if err != nil {
		return nil, fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", filepath.Base(audioPath))
	io.Copy(part, f)
	w.WriteField("segments", string(segJSON))
	w.Close()

	resp, err := http.Post(emotionURL, w.FormDataContentType(), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res EmotionResponse
	json.NewDecoder(resp.Body).Decode(&res)
	return res.Segments, nil
}

// ─── Step 6: VLM analysis ────────────────────────────────────────────────────

// VLM content block types for the OpenAI-compatible multimodal API.
type contentBlock struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type vllmMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type vllmRequest struct {
	Model     string        `json:"model"`
	Messages  []vllmMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

type vllmResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func analyzeVideoWithVLM(framePaths []string, transcript *TranscriptionResponse, emotions []EmotionSegment) (string, error) {
	// Build image content blocks (one per frame).
	var blocks []contentBlock
	for _, fp := range framePaths {
		b64, err := encodeImageBase64(fp)
		if err != nil {
			log.Printf("Skipping frame %s: %v", fp, err)
			continue
		}
		blocks = append(blocks, contentBlock{
			Type:     "image_url",
			ImageURL: &imageURL{URL: "data:image/jpeg;base64," + b64},
		})
	}

	// Build the text prompt with transcript and emotion context.
	contextText := buildContextText(transcript, emotions, len(framePaths))
	blocks = append(blocks, contentBlock{Type: "text", Text: contextText})

	req := vllmRequest{
		Model: vllmModel,
		Messages: []vllmMessage{
			{
				Role:    "system",
				Content: []contentBlock{{Type: "text", Text: systemPrompt}},
			},
			{
				Role:    "user",
				Content: blocks,
			},
		},
		MaxTokens: 3000,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal vllm request: %w", err)
	}

	resp, err := http.Post(vllmURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("vllm request: %w", err)
	}
	defer resp.Body.Close()

	var res vllmResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("decode vllm response: %w", err)
	}
	if len(res.Choices) == 0 {
		return "", fmt.Errorf("empty vllm response")
	}
	return res.Choices[0].Message.Content, nil
}

const systemPrompt = `You are an expert video analyst. You will be given a sequence of video frames (in chronological order) along with the audio transcript and emotion labels detected from the audio.

Your task is to produce a comprehensive, structured analysis of the video that will serve as the knowledge base for a Q&A chatbot. Include:
1. **Overview**: What is this video about? Who are the speakers/subjects?
2. **Timeline**: Key events, topics, and scene changes with approximate timestamps.
3. **Speaker Analysis**: Who speaks, when, and what are the key statements?
4. **Emotional Arc**: How does the emotional tone evolve? Note any significant moments (shouting, distress, joy, etc.) with timestamps.
5. **Visual Details**: Describe important visual elements — setting, objects, text on screen, expressions, actions.
6. **Key Quotes**: The most important or notable statements from the transcript.

Be specific, factual, and reference timestamps where possible.`

func buildContextText(transcript *TranscriptionResponse, emotions []EmotionSegment, frameCount int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("The video has %d frames (0.5 fps, so frame N ≈ timestamp N×2 seconds).\n\n", frameCount))

	// Transcript
	sb.WriteString("## Audio Transcript (with timestamps)\n")
	if transcript != nil && len(transcript.Segments) > 0 {
		for _, seg := range transcript.Segments {
			sb.WriteString(fmt.Sprintf("[%.1fs–%.1fs] %s\n", seg.Start, seg.End, seg.Text))
		}
	} else {
		sb.WriteString("(No speech detected)\n")
	}

	// Emotions
	if len(emotions) > 0 {
		sb.WriteString("\n## Audio Emotion Labels (per speech segment)\n")
		for _, e := range emotions {
			if e.Confidence > 0.4 { // Only include reasonably confident predictions
				sb.WriteString(fmt.Sprintf("[%.1fs–%.1fs] %s (%.0f%% confidence)\n",
					e.Start, e.End, strings.ToUpper(e.Emotion), e.Confidence*100))
			}
		}
	}

	sb.WriteString("\n## Your Task\nAnalyze the video frames above together with the transcript and emotion data. Produce the comprehensive analysis as described in your instructions.")
	return sb.String()
}

// ─── Step 7: Extract summary from analysis ──────────────────────────────────

// extractSummaryFromAnalysis sends a short follow-up to the LLM to condense
// the full analysis into a 2-3 sentence summary for the video card UI.
func extractSummaryFromAnalysis(analysis string) string {
	if analysis == "" {
		return ""
	}

	type textMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": vllmModel,
		"messages": []textMsg{
			{Role: "system", Content: "You are a helpful assistant. Summarize in 2-3 sentences."},
			{Role: "user", Content: "Summarize the following video analysis:\n\n" + analysis},
		},
		"max_tokens": 150,
	})

	resp, err := http.Post(vllmURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return analysis[:min(len(analysis), 300)]
	}
	defer resp.Body.Close()

	var res vllmResponse
	json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Choices) > 0 {
		return res.Choices[0].Message.Content
	}
	return ""
}

// ─── Chat helper (used by GraphQL resolver via the same VLM) ─────────────────

// ChatWithVideo sends a user question to the VLM using the stored video analysis
// as the system context and the prior conversation history.
func ChatWithVideo(analysis string, history []ChatTurn, userMessage string) (string, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	systemContent := fmt.Sprintf(`You are a helpful assistant that answers questions about a specific video.
Use the following comprehensive video analysis as your knowledge base. Answer questions accurately,
citing timestamps and specific details from the analysis when relevant.

## Video Analysis
%s`, analysis)

	messages := []msg{{Role: "system", Content: systemContent}}
	for _, turn := range history {
		messages = append(messages, msg{Role: turn.Role, Content: turn.Content})
	}
	messages = append(messages, msg{Role: "user", Content: userMessage})

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":      vllmModel,
		"messages":   messages,
		"max_tokens": 800,
	})

	resp, err := http.Post(vllmURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("vllm chat request: %w", err)
	}
	defer resp.Body.Close()

	var res vllmResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if len(res.Choices) == 0 {
		return "", fmt.Errorf("empty response from VLM")
	}
	return res.Choices[0].Message.Content, nil
}

// ChatTurn represents a single message in a conversation.
type ChatTurn struct {
	Role    string
	Content string
}

// ─── Utilities ───────────────────────────────────────────────────────────────

func encodeImageBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func listSortedFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var paths []string
	for _, e := range entries {
		if !e.IsDir() {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}

func setStatus(ctx context.Context, db *storage.DB, videoID, status string) {
	_, _ = db.Pool.Exec(ctx, "UPDATE videos SET status=$1 WHERE id=$2", status, videoID)
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Command %s failed: %v — %s", name, err, out)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
