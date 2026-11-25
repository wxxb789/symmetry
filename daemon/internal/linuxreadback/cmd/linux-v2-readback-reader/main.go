// Command linux-v2-readback-reader serves the acceptance fixture's bounded
// root readback channel for one case set.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/linuxreadback"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: linux-v2-readback-reader serve --channel DIR --leaf-root DIR --a-uid N --a-gid N --b-uid N --b-gid N")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	channel := flags.String("channel", "", "root:A channel directory")
	leafRoot := flags.String("leaf-root", "", "fixed operator cgroup root the request leaf must stay below")
	aUID := flags.Uint("a-uid", 0, "A identity whose requests are served")
	aGID := flags.Uint("a-gid", 0, "A primary group")
	bUID := flags.Uint("b-uid", 0, "B identity a target must carry")
	bGID := flags.Uint("b-gid", 0, "B primary group")
	_ = flags.Parse(os.Args[2:])
	if *channel == "" || *leafRoot == "" || *aUID == 0 || *aGID == 0 || *bUID == 0 || *bGID == 0 {
		fmt.Fprintln(os.Stderr, "linux-v2-readback-reader: channel, leaf root and both identities are required")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	options := linuxreadback.ServeOptions{
		Channel: *channel, LeafRoot: *leafRoot,
		AUID: uint32(*aUID), AGID: uint32(*aGID), BUID: uint32(*bUID), BGID: uint32(*bGID),
	}
	if err := linuxreadback.Serve(ctx, options); err != nil {
		fmt.Fprintf(os.Stderr, "linux-v2-readback-reader: %v\n", err)
		os.Exit(1)
	}
}
