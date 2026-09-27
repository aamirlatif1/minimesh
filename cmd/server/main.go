package main

import (
	"flag"
	"io"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/echo", echoHandler)

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// echoHandler writes the request body back to the client. For requests
// without a body (e.g. GET), it echoes the "msg" query parameter instead.
func echoHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	if ct := r.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}

	if r.Body != nil && r.ContentLength != 0 {
		if _, err := io.Copy(w, r.Body); err != nil {
			log.Printf("echo: %v", err)
		}
		return
	}
	io.WriteString(w, r.URL.Query().Get("msg"))
}
