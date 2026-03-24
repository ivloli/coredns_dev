package main

import (
	// Load all standard CoreDNS plugins (log, errors, forward, cache, etc.)
	_ "github.com/coredns/coredns/core/plugin"
	"github.com/coredns/coredns/coremain"

	// Our custom plugin — registers itself via init()
	_ "coredns-dev/coredns/plugin/ecs_normalizer"
)

func main() {
	coremain.Run()
}
