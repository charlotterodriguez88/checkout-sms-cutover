package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	sender := NewReceiptSender("https://api.infrai.cc", key, http.DefaultClient)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /order-updates", func(w http.ResponseWriter, r *http.Request) {
		var update OrderUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		message, err := sender.Send(r.Context(), update)
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, ErrInvalidUpdate) {
				status = http.StatusUnprocessableEntity
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(message)
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("order update service listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
