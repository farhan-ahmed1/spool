package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/redis/go-redis/v9"
)

// ImageProcessingTask represents different image processing operations
type ImageProcessingTask struct {
	Operation string // "resize", "grayscale", "thumbnail", "watermark"
	InputPath string
	OutputPath string
	Width   int
	Height   int
	Quality  int
}

func main() {
	log.Println(" Starting Image Processor Demo")
	log.Println("================================")

	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:  0,
	})

	// Test connection
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf(" Failed to connect to Redis: %v", err)
	}
	log.Println(" Connected to Redis")

	// Initialize components
	q, err := queue.NewRedisQueue("localhost:6379", "", 0, 10)
	if err != nil {
		log.Fatalf(" Failed to create queue: %v", err)
	}
	store := storage.NewRedisStorage(redisClient)
	registry := task.NewRegistry()

	// Register image processing handlers
	registerHandlers(registry)
	log.Println(" Registered image processing handlers")

	// Create output directory
	outputDir := "./output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf(" Failed to create output directory: %v", err)
	}

	// Start workers
	numWorkers := 3
	workers := make([]*worker.Worker, numWorkers)

	for i := 0; i < numWorkers; i++ {
		w := worker.NewWorker(q, store, registry, worker.Config{
			ID:      fmt.Sprintf("image-worker-%d", i+1),
			PollInterval: 100 * time.Millisecond,
		})

		if err := w.Start(ctx); err != nil {
			log.Fatalf(" Failed to start worker %d: %v", i+1, err)
		}
		workers[i] = w
		log.Printf(" Started worker: image-worker-%d", i+1)
	}

	// Generate sample images
	log.Println("\n📸 Generating sample images...")
	sampleImages := generateSampleImages(outputDir)
	log.Printf(" Generated %d sample images", len(sampleImages))

	// Submit image processing tasks
	log.Println("\n📤 Submitting image processing tasks...")
	taskIDs := submitImageTasks(ctx, q, sampleImages, outputDir)
	log.Printf(" Submitted %d tasks", len(taskIDs))

	// Monitor task progress
	log.Println("\n Monitoring task progress...")
	monitorTasks(ctx, store, taskIDs)

	// Wait for tasks to complete
	time.Sleep(5 * time.Second)

	// Display results
	displayResults(ctx, store, taskIDs, outputDir)

	// Graceful shutdown
	log.Println("\n🛑 Shutting down workers...")

	for i, w := range workers {
		if err := w.Stop(); err != nil {
			log.Printf(" Worker %d shutdown error: %v", i+1, err)
		}
	}

	log.Println(" All workers stopped")
	log.Println("\n🎉 Demo complete! Check the ./output directory for processed images.")
}

// registerHandlers registers all image processing task handlers
func registerHandlers(registry *task.Registry) {
	// Grayscale conversion
	registry.Register("image_grayscale", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		inputPath := data["input_path"].(string)
		outputPath := data["output_path"].(string)

		log.Printf("Converting to grayscale: %s", filepath.Base(inputPath))

		img, err := loadImage(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load image: %w", err)
		}

		grayImg := convertToGrayscale(img)

		if err := saveImage(outputPath, grayImg); err != nil {
			return nil, fmt.Errorf("failed to save image: %w", err)
		}

		return map[string]string{"output": outputPath}, nil
	})

	// Resize operation
	registry.Register("image_resize", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		inputPath := data["input_path"].(string)
		outputPath := data["output_path"].(string)
		width := int(data["width"].(float64))
		height := int(data["height"].(float64))

		log.Printf("Resizing to %dx%d: %s", width, height, filepath.Base(inputPath))

		img, err := loadImage(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load image: %w", err)
		}

		resizedImg := resizeImage(img, width, height)

		if err := saveImage(outputPath, resizedImg); err != nil {
			return nil, fmt.Errorf("failed to save image: %w", err)
		}

		return map[string]string{"output": outputPath}, nil
	})

	// Thumbnail generation (high priority)
	registry.Register("image_thumbnail", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		inputPath := data["input_path"].(string)
		outputPath := data["output_path"].(string)

		log.Printf("Generating thumbnail: %s", filepath.Base(inputPath))

		img, err := loadImage(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load image: %w", err)
		}

		thumbnail := resizeImage(img, 150, 150)

		if err := saveImage(outputPath, thumbnail); err != nil {
			return nil, fmt.Errorf("failed to save image: %w", err)
		}

		return map[string]string{"output": outputPath}, nil
	})

	// Add watermark
	registry.Register("image_watermark", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		inputPath := data["input_path"].(string)
		outputPath := data["output_path"].(string)

		log.Printf("Adding watermark: %s", filepath.Base(inputPath))

		img, err := loadImage(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load image: %w", err)
		}

		watermarkedImg := addWatermark(img)

		if err := saveImage(outputPath, watermarkedImg); err != nil {
			return nil, fmt.Errorf("failed to save image: %w", err)
		}

		return map[string]string{"output": outputPath}, nil
	})
}

// generateSampleImages creates sample images for processing
func generateSampleImages(outputDir string) []string {
	images := []string{}

	for i := 1; i <= 5; i++ {
		filename := filepath.Join(outputDir, fmt.Sprintf("sample_%d.png", i))
		img := createSampleImage(400, 300, i)

		if err := saveImage(filename, img); err != nil {
			log.Printf(" Failed to create sample image %d: %v", i, err)
			continue
		}

		images = append(images, filename)
	}

	return images
}

// submitImageTasks submits various image processing tasks
func submitImageTasks(ctx context.Context, q queue.Queue, images []string, outputDir string) []string {
	taskIDs := []string{}

	for i, imagePath := range images {
		// Submit thumbnail task (high priority)
		thumbnailTask, err := task.NewTask("image_thumbnail", map[string]interface{}{
			"input_path": imagePath,
			"output_path": filepath.Join(outputDir, fmt.Sprintf("thumbnail_%d.png", i+1)),
		})
		if err != nil {
			log.Printf(" Failed to create thumbnail task: %v", err)
			continue
		}
		thumbnailTask.WithPriority(task.PriorityHigh)

		if err := q.Enqueue(ctx, thumbnailTask); err != nil {
			log.Printf(" Failed to submit thumbnail task: %v", err)
		} else {
			taskIDs = append(taskIDs, thumbnailTask.ID)
		}

		// Submit grayscale task (normal priority)
		grayscaleTask, err := task.NewTask("image_grayscale", map[string]interface{}{
			"input_path": imagePath,
			"output_path": filepath.Join(outputDir, fmt.Sprintf("grayscale_%d.png", i+1)),
		})
		if err != nil {
			log.Printf(" Failed to create grayscale task: %v", err)
			continue
		}
		grayscaleTask.WithPriority(task.PriorityNormal)

		if err := q.Enqueue(ctx, grayscaleTask); err != nil {
			log.Printf(" Failed to submit grayscale task: %v", err)
		} else {
			taskIDs = append(taskIDs, grayscaleTask.ID)
		}

		// Submit resize task (low priority)
		resizeTask, err := task.NewTask("image_resize", map[string]interface{}{
			"input_path": imagePath,
			"output_path": filepath.Join(outputDir, fmt.Sprintf("resized_%d.png", i+1)),
			"width":    float64(800),
			"height":   float64(600),
		})
		if err != nil {
			log.Printf(" Failed to create resize task: %v", err)
			continue
		}
		resizeTask.WithPriority(task.PriorityLow)

		if err := q.Enqueue(ctx, resizeTask); err != nil {
			log.Printf(" Failed to submit resize task: %v", err)
		} else {
			taskIDs = append(taskIDs, resizeTask.ID)
		}

		// Submit watermark task (normal priority)
		watermarkTask, err := task.NewTask("image_watermark", map[string]interface{}{
			"input_path": imagePath,
			"output_path": filepath.Join(outputDir, fmt.Sprintf("watermarked_%d.png", i+1)),
			"text":    "© Spool Demo",
		})
		if err != nil {
			log.Printf(" Failed to create watermark task: %v", err)
			continue
		}
		watermarkTask.WithPriority(task.PriorityNormal)

		if err := q.Enqueue(ctx, watermarkTask); err != nil {
			log.Printf(" Failed to submit watermark task: %v", err)
		} else {
			taskIDs = append(taskIDs, watermarkTask.ID)
		}
	}

	return taskIDs
}

// monitorTasks displays real-time progress of tasks
func monitorTasks(ctx context.Context, store storage.Storage, taskIDs []string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeout := time.After(15 * time.Second)

	for {
		select {
		case <-ticker.C:
			completed := 0
			failed := 0

			for _, id := range taskIDs {
				result, err := store.GetResult(ctx, id)
				if err == nil && result != nil {
					if result.Success {
						completed++
					} else if result.Error != "" {
						failed++
					}
				}
			}

			processing := len(taskIDs) - completed - failed

			if completed+failed == len(taskIDs) {
				log.Printf(" Progress: %d completed, %d failed, %d processing", completed, failed, processing)
				return
			}

			log.Printf(" Progress: %d completed, %d failed, %d processing", completed, failed, processing)

		case <-timeout:
			log.Println(" Monitoring timeout")
			return
		}
	}
}

// displayResults shows the final results of all tasks
func displayResults(ctx context.Context, store storage.Storage, taskIDs []string, outputDir string) {
	log.Println("\n📋 Task Results:")
	log.Println("================")

	completed := 0
	failed := 0

	for _, id := range taskIDs {
		result, err := store.GetResult(ctx, id)
		if err != nil {
			log.Printf(" Task %s: unable to get result", id[:8])
			continue
		}

		if result == nil {
			log.Printf("⏳ Task %s: still processing", id[:8])
			continue
		}

		if result.Success {
			completed++
			log.Printf(" Task %s: completed in %v", id[:8], result.Duration)
		} else {
			failed++
			log.Printf(" Task %s: failed - %v", id[:8], result.Error)
		}
	}

	log.Println("\n Summary:")
	log.Printf(" Total tasks: %d", len(taskIDs))
	log.Printf(" Completed: %d (%.1f%%)", completed, float64(completed)/float64(len(taskIDs))*100)
	log.Printf(" Failed: %d (%.1f%%)", failed, float64(failed)/float64(len(taskIDs))*100)

	// List output files
	log.Println("\n📁 Output files:")
	files, err := filepath.Glob(filepath.Join(outputDir, "*"))
	if err == nil {
		for _, file := range files {
			info, err := os.Stat(file)
			if err == nil && !info.IsDir() {
				log.Printf(" - %s (%d bytes)", filepath.Base(file), info.Size())
			}
		}
	}
}

// Image processing utilities

func createSampleImage(width, height, seed int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Create a gradient pattern based on seed
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r := uint8((x + seed*50) % 256)
			g := uint8((y + seed*30) % 256)
			b := uint8((x + y + seed*70) % 256)
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}

	return img
}

func loadImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Try PNG first
	img, err := png.Decode(file)
	if err != nil {
		// Try JPEG
		file.Seek(0, 0)
		img, err = jpeg.Decode(file)
		if err != nil {
			return nil, err
		}
	}

	return img, nil
}

func saveImage(path string, img image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Save as PNG
	return png.Encode(file, img)
}

func convertToGrayscale(img image.Image) image.Image {
	bounds := img.Bounds()
	grayImg := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			grayImg.Set(x, y, color.GrayModel.Convert(img.At(x, y)))
		}
	}

	return grayImg
}

func resizeImage(img image.Image, width, height int) image.Image {
	bounds := img.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()

	resized := image.NewRGBA(image.Rect(0, 0, width, height))

	// Simple nearest-neighbor resize
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := x * srcWidth / width
			srcY := y * srcHeight / height
			resized.Set(x, y, img.At(srcX+bounds.Min.X, srcY+bounds.Min.Y))
		}
	}

	return resized
}

func addWatermark(img image.Image) image.Image {
	bounds := img.Bounds()
	watermarked := image.NewRGBA(bounds)

	// Copy original image
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			watermarked.Set(x, y, img.At(x, y))
		}
	}

	// Add simple watermark (semi-transparent overlay at bottom)
	watermarkHeight := 30
	for y := bounds.Max.Y - watermarkHeight; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			original := watermarked.At(x, y).(color.RGBA)
			// Darken the bottom area
			watermarked.Set(x, y, color.RGBA{
				R: original.R / 2,
				G: original.G / 2,
				B: original.B / 2,
				A: 255,
			})
		}
	}

	return watermarked
}
