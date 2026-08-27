package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("STATUS_PORT")
	if port == "" {
		port = "8080"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "health check request failed: %v\n", err)
		os.Exit(1)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(os.Stderr, "health check returned HTTP %d\n", response.StatusCode)
		os.Exit(1)
	}
}
