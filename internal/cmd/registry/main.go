// Command registry registers the contracts schemas in Apicurio Registry, or checks them against it.
//
//	registry [-url URL] [-version V] register   create or update every artifact, with BACKWARD and FULL validity rules
//	registry [-url URL] check                   dry-run every schema against its latest registered version
//
// Run it from the repository root; make register and make check-registry do, with a port-forward to k3d-hotl.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/HumanOnLoop/contracts/internal/registry"
	"github.com/HumanOnLoop/contracts/internal/schema"
)

func main() {
	log.SetFlags(0)
	url := flag.String("url", envOr("HOTL_REGISTRY_URL", "http://localhost:18080/apis/registry/v3"), "registry v3 API base URL")
	version := flag.String("version", "dev", "contracts version, recorded as a label on every version this run creates")
	flag.Parse()
	if flag.NArg() != 1 || (flag.Arg(0) != "register" && flag.Arg(0) != "check") {
		log.Fatal("usage: registry [-url URL] [-version V] register|check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fsys := os.DirFS(".")
	arts, err := schema.Artifacts(fsys)
	if err != nil {
		log.Fatal(err)
	}
	client := &registry.Client{BaseURL: *url}
	failed := 0
	for _, a := range arts {
		content, err := schema.Bundle(fsys, a.Path)
		if err != nil {
			log.Fatal(err)
		}
		name := a.Group + "/" + a.ID
		switch flag.Arg(0) {
		case "register":
			outcome, v, err := client.Register(ctx, a.Group, a.ID, content, map[string]string{"contracts-version": *version})
			if err != nil {
				failed++
				report(name, err)
				continue
			}
			fmt.Printf("%-40s %s (version %s)\n", name, outcome, v)
		case "check":
			if err := client.Check(ctx, a.Group, a.ID, content); err != nil {
				failed++
				report(name, err)
				continue
			}
			fmt.Printf("%-40s compatible\n", name)
		}
	}
	if failed > 0 {
		log.Fatalf("%d of %d schemas failed", failed, len(arts))
	}
	fmt.Printf("%d schemas OK\n", len(arts))
}

func report(name string, err error) {
	var rv *registry.RuleViolation
	if errors.As(err, &rv) {
		fmt.Fprintf(os.Stderr, "%-40s REFUSED\n", name)
		for _, c := range rv.Causes {
			fmt.Fprintf(os.Stderr, "    %s\n", c)
		}
		return
	}
	fmt.Fprintf(os.Stderr, "%-40s ERROR %v\n", name, err)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
