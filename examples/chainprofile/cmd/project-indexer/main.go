package main

import (
	"os"

	"github.com/0xmhha/indexer-go/cmd/cli"
	_ "github.com/0xmhha/indexer-go/examples/chainprofile/pkg/chains/projectarc"
)

func main() { os.Exit(cli.Main(cli.WithName("project-indexer"))) }
