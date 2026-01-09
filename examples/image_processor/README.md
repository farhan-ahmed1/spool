# Image Processor Demo

This example demonstrates how to use Spool to build a concurrent image processing pipeline with multiple workers.

## Features Demonstrated

- **Multiple Task Types**: Grayscale, resize, thumbnail, watermark operations
- **Priority Queues**: Thumbnails processed with high priority
- **Concurrent Processing**: Multiple workers process images in parallel
- **Task Monitoring**: Real-time progress tracking
- **Result Storage**: Task results persisted in Redis

## What It Does

The demo:

1. Generates 5 sample colorful gradient images
2. Submits 20 image processing tasks (4 per image):
    - Thumbnail generation (high priority) - 150x150px
    - Grayscale conversion (normal priority)
    - Resize to 800x600 (low priority)
    - Watermark addition (normal priority)
3. Processes tasks using 3 concurrent workers
4. Monitors progress in real-time
5. Displays results and timing statistics

## Prerequisites

- Redis running on `localhost:6379`
- Go 1.21 or higher

## Running the Demo

```bash
# Start Redis (from project root)
make docker-up

# Run the demo
cd examples/image_processor
go run main.go
```

## Expected Output

```bash
 Starting Image Processor Demo
================================
 Connected to Redis
 Registered image processing handlers
 Started worker: image-worker-1
 Started worker: image-worker-2
 Started worker: image-worker-3

📸 Generating sample images...
 Generated 5 sample images

📤 Submitting image processing tasks...
 Submitted 20 tasks

 Monitoring task progress...
 Progress: 8 completed, 0 failed, 12 processing
 Progress: 15 completed, 0 failed, 5 processing
 Progress: 20 completed, 0 failed, 0 processing

📋 Task Results:
================
 Task a1b2c3d4: completed in 234ms
 Task e5f6g7h8: completed in 187ms
...

 Summary:
 Total tasks: 20
 Completed: 20 (100.0%)
 Failed: 0 (0.0%)

📁 Output files:
 - sample_1.png (158432 bytes)
 - thumbnail_1.png (12487 bytes)
 - grayscale_1.png (89234 bytes)
 - resized_1.png (245678 bytes)
 - watermarked_1.png (162543 bytes)
 ...

🎉 Demo complete! Check the ./output directory for processed images.
```

## Output Files

The demo creates an `./output` directory containing:

- **sample_N.png** - Original generated images
- **thumbnail_N.png** - 150x150 thumbnails
- **grayscale_N.png** - Grayscale versions
- **resized_N.png** - 800x600 resized images
- **watermarked_N.png** - Images with watermark overlay

## Key Concepts

### Task Registration

```go
registry.Register("image_grayscale", func(ctx context.Context, t *task.Task) error {
  inputPath := t.Payload["input_path"].(string)
  outputPath := t.Payload["output_path"].(string)
  
  // Load, process, save
  img, _ := loadImage(inputPath)
  grayImg := convertToGrayscale(img)
  return saveImage(outputPath, grayImg)
})
```

### Priority Assignment

```go
// High priority for thumbnails (user-facing)
thumbnailTask := task.New("image_thumbnail", payload)
thumbnailTask.SetPriority(task.PriorityHigh)

// Low priority for batch resizing
resizeTask := task.New("image_resize", payload)
resizeTask.SetPriority(task.PriorityLow)
```

### Concurrent Workers

```go
for i := 0; i < numWorkers; i++ {
  w := worker.NewWorker(q, store, registry, worker.Config{
    ID:      fmt.Sprintf("image-worker-%d", i+1),
    PollInterval: 100 * time.Millisecond,
  })
  w.Start(ctx)
}
```

## Customization

### Add New Operations

Add a new task type in `registerHandlers()`:

```go
registry.Register("image_blur", func(ctx context.Context, t *task.Task) error {
  // Your blur implementation
  return nil
})
```

### Adjust Worker Count

Change the number of concurrent workers:

```go
numWorkers := 5 // More workers = faster processing
```

### Modify Image Sizes

Update the resize dimensions:

```go
resizeTask := task.New("image_resize", map[string]interface{}{
  "width": float64(1920),
  "height": float64(1080),
})
```

## Real-World Use Cases

This pattern is useful for:

- **Photo upload processing**: Generate thumbnails and multiple sizes
- **Video thumbnail extraction**: Process video frames in parallel
- **Document conversion**: Convert PDFs to images with different resolutions
- **Batch image optimization**: Compress and optimize large image sets
- **ML preprocessing**: Prepare images for machine learning pipelines

## Performance

With 3 workers processing 20 tasks:

- **Throughput**: ~100-150 images/minute
- **Avg processing time**: 150-300ms per task
- **Memory usage**: ~30MB for workers + image buffers

Scale up workers for higher throughput:

- 5 workers: ~250 images/minute
- 10 workers: ~400 images/minute

## Next Steps

1. Add more sophisticated image processing:
      - Use `github.com/disintegration/imaging` for advanced operations
      - Implement edge detection, filters, or transformations
2. Integrate with cloud storage:
      - Upload processed images to S3/Azure Blob
      - Download source images from URLs
3. Add progress callbacks:
      - Send webhooks on task completion
      - Update database records with results
4. Implement batching:
      - Process multiple images in a single task
      - Use pipeline patterns for multi-stage processing

## Troubleshooting

**Redis connection error**:

```bash
make docker-up # Start Redis
redis-cli ping # Test connection
```

**Out of memory**:

- Reduce number of workers
- Process smaller batches
- Resize images before processing

**Slow processing**:

- Increase worker count
- Adjust poll interval
- Use more efficient image libraries

## Learn More

- [Spool Documentation](../../docs/)
- [Task Priority Guide](../../docs/02_QUICK_REFERENCE.md)
- [Worker Configuration](../../docs/09_CONFIGURATION.md)
