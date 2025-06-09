package main

import (
	"flag"
	"log"
	"net/http"
	"server/internal/signaling"
)

var addr = flag.String("addr", "0.0.0.0:8080", "address to listen on")

func main() {
	flag.Parse()
	hub := signaling.NewHub()
	go hub.Run()
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		signaling.ServerWs(hub, w, r)
	})
	err := http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
