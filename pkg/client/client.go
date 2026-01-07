package client

import (
	"context"
	"fmt"
	"time"

	"github.com/farhan-ahmed1/spool/internal/task"
)

// Config holds client configuration
type Config struct {
	BrokerAddr string
	Timeout    time.Duration
}

// Client provides an interface to interact with the task queue
type Client struct {
	config Config
	// TODO: Add Redis client connection
}

// New creates a new client instance
func New(config Config) (*Client, error) {
	if config.BrokerAddr == "" {
		return nil, fmt.Errorf("broker address is required")
	}

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	// TODO: Initialize Redis connection

	return &Client{
		config: config,
	}, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	// TODO: Close Redis connection
	return nil
}

// Submit submits a task to the queue
func (c *Client) Submit(ctx context.Context, t *task.Task) (string, error) {
	// TODO: Implement task submission to Redis
	return t.ID, fmt.Errorf("not implemented yet")
}

// SubmitBatch submits multiple tasks to the queue
func (c *Client) SubmitBatch(ctx context.Context, tasks []*task.Task) ([]string, error) {
	// TODO: Implement batch submission
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids, fmt.Errorf("not implemented yet")
}

// GetResult retrieves the result of a completed task
func (c *Client) GetResult(ctx context.Context, taskID string, timeout time.Duration) (*task.Result, error) {
	// TODO: Implement result retrieval from Redis
	return nil, fmt.Errorf("not implemented yet")
}

// GetStatus retrieves the current status of a task
func (c *Client) GetStatus(ctx context.Context, taskID string) (*task.Task, error) {
	// TODO: Implement status retrieval from Redis
	return nil, fmt.Errorf("not implemented yet")
}

// Stats holds queue statistics
type Stats struct {
	Pending       int
	Processing    int
	Completed     int
	Failed        int
	ActiveWorkers int
}

// GetStats retrieves queue statistics
func (c *Client) GetStats(ctx context.Context) (*Stats, error) {
	// TODO: Implement stats retrieval from Redis
	return &Stats{}, fmt.Errorf("not implemented yet")
}
