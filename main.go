package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	hub := NewHub()
	runner := NewRunner()
	// Every event goes to the browsers and to the scenario runner.
	network := NewNetwork(func(e Event) {
		hub.Emit(e)
		runner.Observe(e)
	})
	if err := SetupDemo(network); err != nil {
		fmt.Println("Setup error:", err)
		return
	}

	// Send metrics to the UI twice a second.
	go func() {
		for range time.Tick(500 * time.Millisecond) {
			network.EmitMetrics()
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	http.Handle("/ws", hub)
	// The scenario starts when the UI calls POST /api/start.
	http.Handle("/api/", NewController(network, runner, time.Sleep))

	fmt.Println("Hollow Signal Web Dashboard running at http://localhost:8080")
	fmt.Println("Press Ctrl+C in the terminal to stop.")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("Server error:", err)
	}
}
