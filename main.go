package main

import (
	"github.com/semaphoreci/artifact/cmd"

	// Register storage backends
	_ "github.com/semaphoreci/artifact/pkg/backend/hubbackend"
	_ "github.com/semaphoreci/artifact/pkg/backend/s3backend"
)

func main() {
	cmd.Execute()
}
