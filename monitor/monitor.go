package monitor

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Monitor handles the failover logic.
type Monitor struct {
	primaryHostAddr      string
	replicatedContainerIDs []string
}

// NewMonitor creates a new Monitor instance from environment variables.
func NewMonitor() (*Monitor, error) {
	primaryHost := os.Getenv("PRIMARY_HOST_ADDR")
	if primaryHost == "" {
		return nil, &ConfigError{"PRIMARY_HOST_ADDR environment variable not set."}
	}

	containerIDsStr := os.Getenv("REPLICATED_CONTAINER_IDS")
	if containerIDsStr == "" {
		return nil, &ConfigError{"REPLICATED_CONTAINER_IDS environment variable not set."}
	}

	return &Monitor{
		primaryHostAddr:      primaryHost,
		replicatedContainerIDs: strings.Split(containerIDsStr, ","),
	}, nil
}

// Run starts the monitoring loop.
func (m *Monitor) Run() {
	log.Println("Starting in monitor mode...")

	const failureThreshold = 3
	const checkInterval = 10 * time.Second
	failureCount := 0

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		log.Printf("Pinging primary host at %s...", m.primaryHostAddr)
		resp, err := http.Get(m.primaryHostAddr)
		if err != nil || (resp != nil && resp.StatusCode >= 500) {
			failureCount++
			log.Printf("Health check failed (%d/%d): %v", failureCount, failureThreshold, err)
		} else {
			if resp != nil {
				resp.Body.Close()
			}
			failureCount = 0
			log.Println("Health check successful.")
		}

		if failureCount >= failureThreshold {
			log.Println("Primary host is down! Triggering failover...")
			m.triggerFailover()
			return // Exit after triggering failover
		}
	}
}

func (m *Monitor) triggerFailover() {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("Failed to create docker client for failover: %s", err)
		return
	}
	defer cli.Close()

	ctx := context.Background()
	for _, id := range m.replicatedContainerIDs {
		log.Printf("Starting container %s...", id)
		if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
			log.Printf("Failed to start container %s: %s", id, err)
		} else {
			log.Printf("Successfully started container %s.", id)
		}
	}
	log.Println("Failover process complete.")
}

// ConfigError is a custom error for configuration issues.
type ConfigError struct {
	message string
}

func (e *ConfigError) Error() string {
	return e.message
}
