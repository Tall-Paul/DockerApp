# Docker Container Lister

This application runs in a Docker container and lists the other containers running on the host system.

## Build the Docker Image

To build the Docker image, run the following command in your terminal:

```bash
docker build -t docker-lister .
```

## Run the Docker Container

To run the container, you need to mount the host's Docker API socket into the container and map the web UI port. The command differs slightly depending on your operating system.

### On Windows

On Windows (using Docker Desktop), the Docker API is exposed via a named pipe.

```bash
docker run --rm -p 8080:8080 -v //./pipe/docker_engine:/var/run/docker.sock docker-lister
```

### On macOS and Linux

On macOS and Linux, the Docker API is exposed via a Unix socket.

```bash
docker run --rm -p 8080:8080 -v /var/run/docker.sock:/var/run/docker.sock docker-lister
```

After running the command, you can access the web UI at [http://localhost:8080](http://localhost:8080).
