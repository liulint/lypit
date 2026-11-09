package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os/exec"
	"time"
)

func openDefaultBrowser(ctx context.Context, url string) error {
	program, args := browserOpener(url)
	return exec.CommandContext(ctx, program, args...).Run()
}

// Bind before opening a browser, so startup errors never open a dead page.
func serveLocalHTTP(ctx context.Context, server *http.Server, autoOpen bool, open func(context.Context, string) error) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	url := "http://" + listener.Addr().String() + "/"
	log.Printf("映序已启动：%s · 无需登录 · SQLite 本地存储", url)
	if autoOpen {
		done := make(chan struct{})
		defer func() { <-done }()
		go func() {
			defer close(done)
			timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := open(timeout, url); err != nil {
				log.Printf("无法自动打开浏览器，请手动访问 %s：%v", url, err)
			}
		}()
	}
	return server.Serve(listener)
}
