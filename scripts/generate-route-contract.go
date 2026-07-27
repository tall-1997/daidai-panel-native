package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"daidai-panel/router"
)

func main() {
	check := flag.Bool("check", false, "verify that the generated contract is current")
	flag.Parse()

	document := router.BuildContractDocument(router.CanonicalServerRoutes(), router.CanonicalMobileRoutes())
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		fatal(err)
	}
	data = append(data, '\n')

	contractPath := filepath.Join("contracts", "backend-api-mobile.json")
	if *check {
		current, err := os.ReadFile(contractPath)
		if err != nil {
			fatal(err)
		}
		if !bytes.Equal(current, data) {
			fatal(fmt.Errorf("%s is stale or contains unclassified route changes; run go run scripts/generate-route-contract.go", contractPath))
		}
		fmt.Printf("route contract is current: %d routes\n", len(document.Routes))
		return
	}

	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(contractPath, data, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("generated %s: %d routes\n", contractPath, len(document.Routes))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
