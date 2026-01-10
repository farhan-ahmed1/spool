package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/farhan-ahmed1/spool/internal/broker"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/redis/go-redis/v9"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Spool Broker...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Redis queue
	q, err := queue.NewRedisQueue("localhost:6379", "", 0, 10)
	if err != nil {
		log.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	// Initialize storage
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	store := storage.NewRedisStorage(redisClient)
	defer store.Close()

	// Create and start broker
	b := broker.NewBroker(broker.Config{
		Addr:    ":8000",
		Queue:   q,
		Storage: store,
	})

	// Handle shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		fmt.Println("\nShutting down broker...")
		cancel()
		if err := b.Stop(ctx); err != nil {
			log.Printf("Error stopping broker: %v", err)
		}
	}()

	log.Println("Broker listening on :8000")
	if err := b.Start(); err != nil {
		log.Printf("Broker stopped: %v", err)
	}
}
