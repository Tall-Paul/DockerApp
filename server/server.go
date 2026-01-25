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
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
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

	fmt.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start server: %s", err)
	}
}

func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to create docker client: %s", err), http.StatusInternalServerError)
		return
	}
	defer cli.Close()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to list containers: %s", err), http.StatusInternalServerError)
		return
	}

	selectedContainers, err := s.store.GetSelectedContainers()
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to get selected containers: %s", err), http.StatusInternalServerError)
		return
	}

	selectedVolumes, err := s.store.GetSelectedVolumes()
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to get selected volumes: %s", err), http.StatusInternalServerError)
		return
	}

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

	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to parse template: %s", err), http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, containerInfos)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to execute template: %s", err), http.StatusInternalServerError)
		return
	}
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

func (s *Server) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		DestinationHost   string `json:"destinationHost"`
		SourceHostAddress string `json:"sourceHostAddress"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if payload.DestinationHost == "" || payload.SourceHostAddress == "" {
		http.Error(w, "Destination and source host addresses cannot be empty", http.StatusBadRequest)
		return
	}

	log.Printf("Replication started for destination: %s", payload.DestinationHost)

	destURL, err := url.Parse(payload.DestinationHost)
	if err != nil {
		http.Error(w, "Invalid destination host URL", http.StatusBadRequest)
		return
	}
	destHost := destURL.Hostname()

	srcCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to create source docker client: %s", err), http.StatusInternalServerError)
		return
	}
	defer srcCli.Close()

	destCli, err := client.NewClientWithOpts(client.WithHost(payload.DestinationHost), client.WithAPIVersionNegotiation())
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to create destination docker client: %s", err), http.StatusInternalServerError)
		return
	}
	defer destCli.Close()

	selectedContainers, err := s.store.GetSelectedContainers()
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to get selected containers: %s", err), http.StatusInternalServerError)
		return
	}
	selectedVolumes, err := s.store.GetSelectedVolumes()
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to get selected volumes: %s", err), http.StatusInternalServerError)
		return
	}

	ctx := context.Background()

	// --- Volume Replication ---
	const transferPort = "9876/tcp"
	for volName := range selectedVolumes {
		log.Printf("Replicating volume: %s", volName)
		srcVol, err := srcCli.VolumeInspect(ctx, volName)
		if err != nil {
			log.Printf("Failed to inspect source volume %s: %s", volName, err)
			continue
		}

		destVol, err := destCli.VolumeCreate(ctx, volume.CreateOptions{
			Name:       srcVol.Name,
			Labels:     srcVol.Labels,
			Driver:     srcVol.Driver,
			DriverOpts: srcVol.Options,
		})
		if err != nil {
			log.Printf("Failed to create destination volume %s: %s", volName, err)
			continue
		}

		receiverCmd := fmt.Sprintf("nc -l -p %s | tar -xzf - -C /volume_data", strings.Split(transferPort, "/")[0])
		receiverPort, _ := nat.NewPort("tcp", strings.Split(transferPort, "/")[0])

		receiverCont, err := destCli.ContainerCreate(ctx, &container.Config{
			Image:        "alpine",
			Cmd:          []string{"sh", "-c", "apk add --no-cache netcat-openbsd && " + receiverCmd},
			ExposedPorts: nat.PortSet{receiverPort: struct{}{}},
		}, &container.HostConfig{
			Mounts: []mount.Mount{
				{Type: mount.TypeVolume, Source: destVol.Name, Target: "/volume_data"},
			},
			PortBindings: nat.PortMap{
				receiverPort: []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: strings.Split(transferPort, "/")[0]}},
			},
		}, nil, nil, "volume_receiver_"+volName)
		if err != nil {
			log.Printf("Failed to create receiver container for volume %s: %s", volName, err)
			continue
		}
		defer destCli.ContainerRemove(ctx, receiverCont.ID, container.RemoveOptions{Force: true})

		if err = destCli.ContainerStart(ctx, receiverCont.ID, container.StartOptions{}); err != nil {
			log.Printf("Failed to start receiver container for volume %s: %s", volName, err)
			continue
		}

		senderCmd := fmt.Sprintf("tar -czf - -C /volume_data . | nc %s %s", destHost, strings.Split(transferPort, "/")[0])
		senderCont, err := srcCli.ContainerCreate(ctx, &container.Config{
			Image: "alpine",
			Cmd:   []string{"sh", "-c", "apk add --no-cache netcat-openbsd && " + senderCmd},
		}, &container.HostConfig{
			Mounts: []mount.Mount{
				{Type: mount.TypeVolume, Source: srcVol.Name, Target: "/volume_data"},
			},
		}, nil, nil, "volume_sender_"+volName)
		if err != nil {
			log.Printf("Failed to create sender container for volume %s: %s", volName, err)
			continue
		}
		defer srcCli.ContainerRemove(ctx, senderCont.ID, container.RemoveOptions{Force: true})

		if err = srcCli.ContainerStart(ctx, senderCont.ID, container.StartOptions{}); err != nil {
			log.Printf("Failed to start sender container for volume %s: %s", volName, err)
			continue
		}

		log.Printf("Waiting for volume transfer to complete for %s...", volName)
		statusCh, errCh := srcCli.ContainerWait(ctx, senderCont.ID, container.WaitConditionNotRunning)
		select {
		case status := <-statusCh:
			log.Printf("Sender container for %s exited with status %d", volName, status.StatusCode)
		case err := <-errCh:
			log.Printf("Error waiting for sender container %s: %s", volName, err)
		}
		log.Printf("Volume transfer for %s complete.", volName)
	}

	// --- Container Replication ---
	var replicatedContainerIDs []string
	for containerID := range selectedContainers {
		log.Printf("Replicating container: %s", containerID)
		srcCont, err := srcCli.ContainerInspect(ctx, containerID)
		if err != nil {
			log.Printf("Failed to inspect source container %s: %s", containerID, err)
			continue
		}

		out, err := destCli.ImagePull(ctx, srcCont.Config.Image, image.PullOptions{})
		if err != nil {
			log.Printf("Failed to pull image %s on destination: %s", srcCont.Config.Image, err)
			continue
		}
		io.Copy(io.Discard, out)
		out.Close()

		var containerName string
		if len(srcCont.Name) > 1 {
			containerName = strings.TrimPrefix(srcCont.Name, "/")
		}

		createdCont, err := destCli.ContainerCreate(ctx, srcCont.Config, srcCont.HostConfig, &network.NetworkingConfig{
			EndpointsConfig: srcCont.NetworkSettings.Networks,
		}, nil, containerName)
		if err != nil {
			log.Printf("Failed to create container %s on destination: %s", containerName, err)
			continue
		}
		replicatedContainerIDs = append(replicatedContainerIDs, createdCont.ID)
	}

	// --- Deploy Monitor Container ---
	if len(replicatedContainerIDs) > 0 {
		log.Println("Deploying monitor container to destination host...")
		monitorContainerName := "dockerapp-monitor"
		_ = destCli.ContainerRemove(ctx, monitorContainerName, container.RemoveOptions{Force: true})

		monitorCont, err := destCli.ContainerCreate(ctx, &container.Config{
			Image: "docker-lister",
			Cmd:   []string{"./docker-lister", "-mode=monitor"},
			Env: []string{
				"PRIMARY_HOST_ADDR=" + payload.SourceHostAddress,
				"REPLICATED_CONTAINER_IDS=" + strings.Join(replicatedContainerIDs, ","),
			},
		}, &container.HostConfig{
			Mounts: []mount.Mount{
				{Type: mount.TypeBind, Source: "/var/run/docker.sock", Target: "/var/run/docker.sock"},
			},
		}, nil, nil, monitorContainerName)

		if err != nil {
			log.Printf("Failed to create monitor container: %s", err)
		} else {
			if err := destCli.ContainerStart(ctx, monitorCont.ID, container.StartOptions{}); err != nil {
				log.Printf("Failed to start monitor container: %s", err)
			} else {
				log.Println("Successfully deployed and started monitor container.")
			}
		}
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
