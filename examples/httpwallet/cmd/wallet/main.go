// A local demonstration server. Static tokens and in-memory balances are not a
// production identity provider or durable payment system.
package main

import (
	"context"
	_ "embed"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cruskell/proofsteer-go/examples/httpwallet/app"
)

//go:embed wallet.html
var walletPage []byte

func main() {
	address := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	handler := app.New(map[string]app.Session{
		"demo-alice":  {Account: "alice"},
		"demo-locked": {Account: "locked", Locked: true},
	}, map[string]int{"alice": 100, "locked": 100})
	demo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(walletPage)
			return
		}
		handler.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: *address, Handler: demo, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("shutdown: %v", err)
			_ = server.Close()
		}
	}()
	log.Printf("demo wallet listening on %s", *address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
