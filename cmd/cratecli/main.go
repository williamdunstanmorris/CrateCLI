// Command cratecli digs for records on Discogs and finds them on YouTube.
package main

import (
	"os"

	"github.com/williamdunstanmorris/cratecli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
