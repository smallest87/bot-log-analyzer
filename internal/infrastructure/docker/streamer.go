package docker

import (
	"bufio"
	"bot-log-analyzer/internal/domain"
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type dockerStreamer struct {
	cli *client.Client
}

func NewDockerStreamer(cli *client.Client) domain.LogStreamer {
	return &dockerStreamer{
		cli: cli,
	}
}

func (d *dockerStreamer) StreamLogs(containerID string, jobs chan<- string) error {
	ctx := context.Background()
	options := container.LogsOptions{
		ShowStdout: true, 
		ShowStderr: true, 
		Follow:     true, 
		Tail:       "100",
	}

	out, err := d.cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return fmt.Errorf("gagal attach ke container %s: %v", containerID, err)
	}
	defer out.Close()

	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		jobs <- scanner.Text()
	}
	return nil
}