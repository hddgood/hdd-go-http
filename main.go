package main

import (
	"my-http/httpd"
)

func main() {
	sm := httpd.NewServeMux()
	sm.HandleFunc("/a1", func(w httpd.ResponseWriter, req *httpd.Request) {
		w.Write([]byte("a1..."))
	})

	svr := httpd.Server{
		Addr:    "127.0.0.1:8080",
		Handler: sm,
	}
	panic(svr.ListenAndServer())
}
