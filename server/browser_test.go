package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBrowserOpensOnlyAfterHTTPListenerReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })}
	opened := make(chan error, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- serveLocalHTTP(ctx, server, true, func(ctx context.Context, url string) error {
			client := &http.Client{Timeout: 5 * time.Second}
			response, err := client.Get(url)
			if err == nil {
				response.Body.Close()
				if response.StatusCode != 200 {
					err = errors.New("page unavailable")
				}
			}
			opened <- err
			return err
		})
	}()
	defer func() {
		cancel()
		shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		server.Shutdown(shutdown)
	}()
	select {
	case err := <-opened:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("browser did not open")
	}
	shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if err := server.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}

func TestBrowserDoesNotOpenOnPortConflict(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	called := false
	err = serveLocalHTTP(context.Background(), &http.Server{Addr: listener.Addr().String()}, true, func(context.Context, string) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("browser opened on failed startup", err)
	}
}

func TestBrowserCommandUsesURLAsSeparateArgument(t *testing.T) {
	url := "http://127.0.0.1:8080/"
	program, args := browserOpener(url)
	if program == "" || len(args) == 0 || args[len(args)-1] != url {
		t.Fatal(program, args)
	}
	if runtime.GOOS == "windows" && (!strings.HasSuffix(program, "rundll32.exe") || len(args) != 2 || args[0] != "url.dll,FileProtocolHandler") {
		t.Fatal(program, args)
	}
}
