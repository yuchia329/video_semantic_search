package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pgvector/pgvector-go"
	"github.com/yuchia329/video_semantic_search/backend/internal/kafka"
	"github.com/yuchia329/video_semantic_search/backend/internal/storage"
)

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
	return &Processor{
		consumer: consumer,
		db:       db,
		s3:       s3,
	}
}

func (p *Processor) Start(ctx context.Context) {
	log.Println("Starting Video Processor worker...")
	p.consumer.Start(ctx, p.handleMessage)
}

func (p *Processor) handleMessage(ctx context.Context, key, value []byte) error {
	var event ProcessVideoEvent
	if err := json.Unmarshal(value, &event); err != nil {
		return err
	}

	log.Printf("Processing video %s (S3 Key: %s)\n", event.VideoID, event.S3Key)

	// Update status
	_, _ = p.db.Pool.Exec(ctx, "UPDATE videos SET status = 'downloading' WHERE id = $1", event.VideoID)

	tempDir, err := os.MkdirTemp("", "video_process_*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	videoPath := filepath.Join(tempDir, "video.mp4")

	if strings.HasPrefix(event.S3Key, "http") {
		// YouTube download
		cmd := exec.Command("yt-dlp", "-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/mp4", "-o", videoPath, event.S3Key)
		if err := cmd.Run(); err != nil {
			log.Printf("yt-dlp error: %v", err)
			return err
		}
	} else {
		// Download from S3
		f, err := os.Create(videoPath)
		if err != nil {
			return fmt.Errorf("failed to create local video file: %w", err)
		}

		err = p.s3.DownloadFile(ctx, event.S3Key, f)
		f.Close()

		if err != nil {
			return fmt.Errorf("failed to download from S3: %w", err)
		}
	}

	_, _ = p.db.Pool.Exec(ctx, "UPDATE videos SET status = 'extracting' WHERE id = $1", event.VideoID)

	// Extract Audio
	audioPath := filepath.Join(tempDir, "audio.wav")
	cmd := exec.Command("ffmpeg", "-i", videoPath, "-vn", "-acodec", "pcm_s16le", "-ar", "16000", "-ac", "1", audioPath)
	if err := cmd.Run(); err != nil {
		log.Printf("ffmpeg audio extract error: %v", err)
	}

	// Extract Frames (1 frame every 5 seconds for example)
	framesDir := filepath.Join(tempDir, "frames")
	os.Mkdir(framesDir, 0755)
	cmd = exec.Command("ffmpeg", "-i", videoPath, "-vf", "fps=1/5", filepath.Join(framesDir, "frame_%04d.jpg"))
	if err := cmd.Run(); err != nil {
		log.Printf("ffmpeg frame extract error: %v", err)
	}

	_, _ = p.db.Pool.Exec(ctx, "UPDATE videos SET status = 'analyzing_audio' WHERE id = $1", event.VideoID)

	// Call Transcription Server (8902)
	transcription, err := transcribeAudio(audioPath)
	if err == nil {
		for _, seg := range transcription.Segments {
			// Get Text Embedding (8901)
			emb, _ := embedText(seg.Text)
			if emb != nil {
				_, _ = p.db.Pool.Exec(ctx, "INSERT INTO transcript_chunks (video_id, start_time, end_time, text, type, embedding) VALUES ($1, $2, $3, $4, 'audio', $5)",
					event.VideoID, seg.Start, seg.End, seg.Text, pgvector.NewVector(emb))
			}
		}
	} else {
		log.Printf("Transcription error: %v", err)
	}

	_, _ = p.db.Pool.Exec(ctx, "UPDATE videos SET status = 'analyzing_visuals' WHERE id = $1", event.VideoID)

	// Call Vision Captioning Server (8903) for each frame, then embed captions via BGE-m3
	files, _ := os.ReadDir(framesDir)
	for i, f := range files {
		timestamp := float32(i * 5) // 1 frame every 5 secs
		framePath := filepath.Join(framesDir, f.Name())
		caption, err := captionImage(framePath)
		if err != nil || caption == "" {
			log.Printf("Frame %d captioning failed: %v", i, err)
			continue
		}
		// Embed the caption text using BGE-m3 (same embedding space as transcript chunks)
		log.Printf("Frame %d caption: %s\n", i, caption)
		emb, _ := embedText(caption)
		log.Printf("Frame %d embedding vector length: %d\n", i, len(emb))
		if emb != nil {
			_, err := p.db.Pool.Exec(ctx, "INSERT INTO visual_chunks (video_id, timestamp, text, embedding) VALUES ($1, $2, $3, $4)",
				event.VideoID, timestamp, caption, pgvector.NewVector(emb))
			if err != nil {
				log.Printf("Failed to insert visual chunk for frame %d: %v", i, err)
			}
		}
	}

	_, _ = p.db.Pool.Exec(ctx, "UPDATE videos SET status = 'summarizing' WHERE id = $1", event.VideoID)

	// Generate Summary with LLM (8900)
	fullText := ""
	if transcription != nil {
		for _, seg := range transcription.Segments {
			fullText += seg.Text + " "
		}
	}
	summary, _ := summarizeText(fullText)

	// Final Update
	_, _ = p.db.Pool.Exec(ctx, "UPDATE videos SET status = 'completed', summary = $1 WHERE id = $2", summary, event.VideoID)

	log.Printf("Finished processing video %s\n", event.VideoID)
	return nil
}

type TranscriptionResponse struct {
	Segments []struct {
		Start float32 `json:"start"`
		End   float32 `json:"end"`
		Text  string  `json:"text"`
	} `json:"segments"`
}

func transcribeAudio(filePath string) (*TranscriptionResponse, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", filepath.Base(filePath))
	io.Copy(part, file)
	writer.Close()

	resp, err := http.Post("http://localhost:8902/transcribe", writer.FormDataContentType(), body)
	if err != nil {
		log.Printf("Transcription request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	var res TranscriptionResponse
	json.NewDecoder(resp.Body).Decode(&res)
	return &res, nil
}

func embedText(text string) ([]float32, error) {
	reqBody, _ := json.Marshal(map[string]string{"text": text})
	resp, err := http.Post("http://localhost:8901/embed", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res struct {
		Embedding []float32 `json:"embedding"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	return res.Embedding, nil
}

func captionImage(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open image file: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", filepath.Base(filePath))
	io.Copy(part, file)
	writer.Close()

	resp, err := http.Post("http://localhost:8903/caption", writer.FormDataContentType(), body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("caption service returned status %d", resp.StatusCode)
	}

	var res struct {
		Caption string `json:"caption"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	return res.Caption, nil
}

func summarizeText(text string) (string, error) {
	if text == "" {
		return "No audio transcript available to summarize.", nil
	}

	// Truncate to avoid context limits if too long
	if len(text) > 10000 {
		text = text[:10000]
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": "Qwen/Qwen3.5-35B-A3B-GPTQ-Int4",
		"messages": []map[string]string{
			{"role": "system", "content": "You are a helpful assistant. Summarize the following video transcript concisely."},
			{"role": "user", "content": text},
		},
		"max_tokens": 200,
	})

	resp, err := http.Post("http://localhost:8900/v1/chat/completions", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Choices) > 0 {
		return res.Choices[0].Message.Content, nil
	}
	return "", nil
}
