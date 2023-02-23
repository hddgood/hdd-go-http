package main

import (
	"fmt"
	"my-http/httpd"
)

func main() {
	e := httpd.New()

	e.HandlerFunc("GET", "/a", func(w httpd.ResponseWriter, r *httpd.Request) {
		w.Write([]byte("go to a"))
	})
	e.HandlerFunc("GET", "/:a", func(w httpd.ResponseWriter, r *httpd.Request) {
		a := r.Query("a")
		w.Write([]byte(fmt.Sprintf("go to :..  %s", a)))
	})
	e.HandlerFunc("GET", "/ab/*a", func(w httpd.ResponseWriter, r *httpd.Request) {
		a := r.Query("a")
		w.Write([]byte(fmt.Sprintf("go to :..  %s", a)))
	})
	svr := httpd.Server{
		Addr:    "127.0.0.1:8080",
		Handler: e,
	}
	panic(svr.ListenAndServer())
}
