package main

import (
	"os"

	connectorapp "github.com/screenleon/agent-native-pm/internal/connector"
)

func main() {
	os.Exit(connectorapp.Run(os.Args[1:], os.Stdout, os.Stderr))
}