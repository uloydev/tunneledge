package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := os.Getenv("TE_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Timestamp", time.Now().UTC().Format(time.RFC3339Nano))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)

		n, err := io.Copy(w, r.Body)
		if err != nil {
			log.Printf("%s %s copy error after %d bytes: %v", r.Method, r.URL.Path, n, err)
			return
		}

		log.Printf("%s %s %d bytes", r.Method, r.URL.Path, n)
	})

	log.Printf("http-echo server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
