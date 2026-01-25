package server

import (
	"context"
	"dockerap/store"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// Server holds the dependencies for the web server.
type Server struct {
	store *store.Store
}

// NewServer creates a new Server instance.
func NewServer(s *store.Store) *Server {
	return &Server{store: s}
}

// Run starts the HTTP server.
func (s *Server) Run() {
	http.HandleFunc("/", s.handleListContainers)
	http.HandleFunc("/select", s.handleSelect)
	http.HandleFunc("/replicate", s.handleReplicate)

	// Destination API endpoints
	http.HandleFunc("/api/pull-image", s.handlePullImage)
	http.HandleFunc("/api/create-container", s.handleCreateContainer)
	http.HandleFunc("/api/create-volume", s.handleCreateVolume)

	fmt.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start server: %s", err)
	}
}

func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("ERROR: Unable to create docker client: %s", err)
		http.Error(w, fmt.Sprintf("Unable to create docker client: %s", err), http.StatusInternalServerError)
		return
	}
	defer cli.Close()

	// Log Docker host and version info
	info, err := cli.Info(context.Background())
	if err != nil {
		log.Printf("ERROR: Unable to get Docker info: %s", err)
	} else {
		log.Printf("Connected to Docker daemon. Containers: %d, Images: %d", info.Containers, info.Images)
	}

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		log.Printf("ERROR: Unable to list containers: %s", err)
		http.Error(w, fmt.Sprintf("Unable to list containers: %s", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully listed %d containers", len(containers))
	for i, c := range containers {
		log.Printf("Container %d: ID=%s, Names=%v, Image=%s", i, c.ID[:12], c.Names, c.Image)
	}

	selectedContainers, err := s.store.GetSelectedContainers()
	if err != nil {
		log.Printf("ERROR: Unable to get selected containers: %s", err)
		http.Error(w, fmt.Sprintf("Unable to get selected containers: %s", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Retrieved %d selected containers from store", len(selectedContainers))

	selectedVolumes, err := s.store.GetSelectedVolumes()
	if err != nil {
		log.Printf("ERROR: Unable to get selected volumes: %s", err)
		http.Error(w, fmt.Sprintf("Unable to get selected volumes: %s", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Retrieved %d selected volumes from store", len(selectedVolumes))

	var containerInfos []ContainerInfo
	for _, c := range containers {
		var mounts []MountInfo
		for _, m := range c.Mounts {
			mounts = append(mounts, MountInfo{
				MountPoint: m,
				IsSelected: selectedVolumes[m.Name],
			})
		}
		containerInfos = append(containerInfos, ContainerInfo{
			ID:         c.ID,
			Names:      c.Names,
			Image:      c.Image,
			State:      c.State,
			Status:     c.Status,
			Mounts:     mounts,
			IsSelected: selectedContainers[c.ID],
		})
	}
	log.Printf("Built %d containerInfos for template", len(containerInfos))

	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Printf("ERROR: Unable to parse template: %s", err)
		http.Error(w, fmt.Sprintf("Unable to parse template: %s", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Template parsed successfully")

	err = tmpl.Execute(w, containerInfos)
	if err != nil {
		log.Printf("ERROR: Unable to execute template: %s", err)
		http.Error(w, fmt.Sprintf("Unable to execute template: %s", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Template executed successfully")
}

func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Name       string `json:"name"`
		IsSelected bool   `json:"isSelected"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateSelection(payload.Type, payload.ID, payload.Name, payload.IsSelected); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Destination API: Pull an image
func (s *Server) handlePullImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		ImageName string `json:"imageName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Pulling image: %s", payload.ImageName)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("ERROR: Unable to create docker client: %s", err)
		http.Error(w, fmt.Sprintf("Unable to create docker client: %s", err), http.StatusInternalServerError)
		return
	}
	defer cli.Close()

	out, err := cli.ImagePull(context.Background(), payload.ImageName, image.PullOptions{})
	if err != nil {
		log.Printf("ERROR: Failed to pull image %s: %s", payload.ImageName, err)
		http.Error(w, fmt.Sprintf("Failed to pull image: %s", err), http.StatusInternalServerError)
		return
	}
	io.Copy(io.Discard, out)
	out.Close()

	log.Printf("Successfully pulled image: %s", payload.ImageName)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// Destination API: Create a container
func (s *Server) handleCreateContainer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Name          string                      `json:"name"`
		Config        *container.Config           `json:"config"`
		HostConfig    *container.HostConfig       `json:"hostConfig"`
		NetworkConfig *network.NetworkingConfig   `json:"networkConfig"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("ERROR: Invalid request body: %s", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Creating container: %s", payload.Name)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("ERROR: Unable to create docker client: %s", err)
		http.Error(w, fmt.Sprintf("Unable to create docker client: %s", err), http.StatusInternalServerError)
		return
	}
	defer cli.Close()

	createdCont, err := cli.ContainerCreate(
		context.Background(),
		payload.Config,
		payload.HostConfig,
		payload.NetworkConfig,
		nil,
		payload.Name,
	)
	if err != nil {
		log.Printf("ERROR: Failed to create container %s: %s", payload.Name, err)
		http.Error(w, fmt.Sprintf("Failed to create container: %s", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully created container: %s (ID: %s)", payload.Name, createdCont.ID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":      "success",
		"containerID": createdCont.ID,
	})
}

// Destination API: Create a volume
func (s *Server) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Name       string            `json:"name"`
		Driver     string            `json:"driver"`
		DriverOpts map[string]string `json:"driverOpts"`
		Labels     map[string]string `json:"labels"`
		VolumeData []byte            `json:"volumeData"` // Base64 encoded tar.gz
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("ERROR: Invalid request body: %s", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Creating volume: %s", payload.Name)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("ERROR: Unable to create docker client: %s", err)
		http.Error(w, fmt.Sprintf("Unable to create docker client: %s", err), http.StatusInternalServerError)
		return
	}
	defer cli.Close()

	ctx := context.Background()

	// Create the volume
	vol, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:       payload.Name,
		Driver:     payload.Driver,
		DriverOpts: payload.DriverOpts,
		Labels:     payload.Labels,
	})
	if err != nil {
		log.Printf("ERROR: Failed to create volume %s: %s", payload.Name, err)
		http.Error(w, fmt.Sprintf("Failed to create volume: %s", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully created volume: %s", vol.Name)

	// TODO: If volumeData is provided, populate the volume
	// This would require creating a temporary container to extract the data

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":     "success",
		"volumeName": vol.Name,
	})
}

func (s *Server) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		DestinationURL    string `json:"destinationHost"` // URL of destination app (e.g., http://5.6.7.8:8080)
		SourceHostAddress string `json:"sourceHostAddress"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if payload.DestinationURL == "" || payload.SourceHostAddress == "" {
		http.Error(w, "Destination and source host addresses cannot be empty", http.StatusBadRequest)
		return
	}

	log.Printf("Replication started for destination: %s", payload.DestinationURL)

	// Get source Docker client
	srcCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("ERROR: Unable to create source docker client: %s", err)
		http.Error(w, fmt.Sprintf("Unable to create source docker client: %s", err), http.StatusInternalServerError)
		return
	}
	defer srcCli.Close()

	// Get selected items from store
	selectedContainers, err := s.store.GetSelectedContainers()
	if err != nil {
		log.Printf("ERROR: Unable to get selected containers: %s", err)
		http.Error(w, fmt.Sprintf("Unable to get selected containers: %s", err), http.StatusInternalServerError)
		return
	}
	selectedVolumes, err := s.store.GetSelectedVolumes()
	if err != nil {
		log.Printf("ERROR: Unable to get selected volumes: %s", err)
		http.Error(w, fmt.Sprintf("Unable to get selected volumes: %s", err), http.StatusInternalServerError)
		return
	}

	ctx := context.Background()
	httpClient := &http.Client{}

	// --- Volume Replication via API ---
	for volName := range selectedVolumes {
		log.Printf("Replicating volume: %s", volName)
		srcVol, err := srcCli.VolumeInspect(ctx, volName)
		if err != nil {
			log.Printf("Failed to inspect source volume %s: %s", volName, err)
			continue
		}

		// Call destination app's API to create volume
		volPayload := map[string]interface{}{
			"name":       srcVol.Name,
			"driver":     srcVol.Driver,
			"driverOpts": srcVol.Options,
			"labels":     srcVol.Labels,
		}
		jsonData, _ := json.Marshal(volPayload)
		resp, err := httpClient.Post(payload.DestinationURL+"/api/create-volume", "application/json", strings.NewReader(string(jsonData)))
		if err != nil {
			log.Printf("Failed to create volume %s on destination: %s", volName, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("Failed to create volume %s on destination: HTTP %d", volName, resp.StatusCode)
			continue
		}

		log.Printf("Successfully replicated volume: %s", volName)
	}

	// --- Container Replication via API ---
	for containerID := range selectedContainers {
		log.Printf("Replicating container: %s", containerID)
		srcCont, err := srcCli.ContainerInspect(ctx, containerID)
		if err != nil {
			log.Printf("Failed to inspect source container %s: %s", containerID, err)
			continue
		}

		// Call destination app's API to pull image
		imgPayload := map[string]string{"imageName": srcCont.Config.Image}
		jsonData, _ := json.Marshal(imgPayload)
		resp, err := httpClient.Post(payload.DestinationURL+"/api/pull-image", "application/json", strings.NewReader(string(jsonData)))
		if err != nil {
			log.Printf("Failed to pull image %s on destination: %s", srcCont.Config.Image, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("Failed to pull image %s on destination: HTTP %d", srcCont.Config.Image, resp.StatusCode)
			continue
		}

		// Call destination app's API to create container
		var containerName string
		if len(srcCont.Name) > 1 {
			containerName = strings.TrimPrefix(srcCont.Name, "/")
		}

		contPayload := map[string]interface{}{
			"name":          containerName,
			"config":        srcCont.Config,
			"hostConfig":    srcCont.HostConfig,
			"networkConfig": &network.NetworkingConfig{EndpointsConfig: srcCont.NetworkSettings.Networks},
		}
		jsonData, _ = json.Marshal(contPayload)
		resp, err = httpClient.Post(payload.DestinationURL+"/api/create-container", "application/json", strings.NewReader(string(jsonData)))
		if err != nil {
			log.Printf("Failed to create container %s on destination: %s", containerName, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("Failed to create container %s on destination: HTTP %d", containerName, resp.StatusCode)
			continue
		}

		log.Printf("Successfully replicated container: %s", containerName)
	}

	log.Println("Replication process finished.")
	w.WriteHeader(http.StatusOK)
}

// These would be unexported helper methods called by handleReplicate
// func (s *Server) replicateVolumes(...)
// func (s *Server) replicateContainers(...)
// func (s *Server) deployMonitor(...)

// --- Data structures for the template ---

type MountInfo struct {
	types.MountPoint
	IsSelected bool
}

type ContainerInfo struct {
	ID         string
	Names      []string
	Image      string
	State      string
	Status     string
	Mounts     []MountInfo
	IsSelected bool
}
