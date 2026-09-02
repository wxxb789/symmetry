// symmetry-daemon loads local daemon configuration and prepares it for startup.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

type startupLog struct {
	Event            string `json:"event"`
	ControlPlaneURL  string `json:"control_plane_url"`
	MachineName      string `json:"machine_name"`
	RuntimeName      string `json:"runtime_name"`
	RuntimeCapacity  int    `json:"runtime_capacity"`
	ConnectionStatus string `json:"connection_status"`
}

func main() {
	configPath := flag.String("config", "", "path to the daemon JSON configuration")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "-config is required")
		os.Exit(2)
	}

	value, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	entry := startupLog{
		Event:            "daemon_configured",
		ControlPlaneURL:  value.ControlPlaneURL,
		MachineName:      value.MachineName,
		RuntimeName:      value.Runtime.Name,
		RuntimeCapacity:  value.Runtime.Capacity,
		ConnectionStatus: "not_connected",
	}
	if err := json.NewEncoder(os.Stdout).Encode(entry); err != nil {
		fmt.Fprintf(os.Stderr, "write startup log: %v\n", err)
		os.Exit(1)
	}
}
