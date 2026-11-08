package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"local/lowcode-gateway/internal/app"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func main() {
	syscall.Umask(0077)
	health := flag.Bool("healthcheck", false, "Lokale API-Bereitschaft prüfen")
	demo := flag.Bool("demo", false, "Lokalen Pilot mit Dummy-API und Testzugängen starten")
	flag.Parse()
	if *health {
		_, port, e := net.SplitHostPort(env("GATEWAY_API_ADDR", "127.0.0.1:18080"))
		if e != nil {
			os.Exit(1)
		}
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
		r, e := client.Get("http://127.0.0.1:" + port + "/healthz")
		if e != nil {
			os.Exit(1)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			os.Exit(1)
		}
		fmt.Println("ready")
		return
	}
	dataDefault := ".data"
	if *demo {
		dataDefault = ".data-demo"
	}
	c := app.Config{DataDir: env("GATEWAY_DATA", dataDefault), AdminAddr: env("GATEWAY_ADMIN_ADDR", "127.0.0.1:19090"), APIAddr: env("GATEWAY_API_ADDR", "127.0.0.1:18080"), AdminOrigin: env("GATEWAY_ADMIN_ORIGIN", "http://127.0.0.1:19090"), Demo: *demo, DemoURL: "http://127.0.0.1:19091", PublicURL: env("GATEWAY_PUBLIC_URL", "http://127.0.0.1:18080")}
	if *demo {
		for _, addr := range []string{c.AdminAddr, c.APIAddr} {
			h, _, e := net.SplitHostPort(addr)
			if e != nil || net.ParseIP(h) == nil || !net.ParseIP(h).IsLoopback() {
				log.Fatal("Demo-Modus ausschließlich mit Loopback-Listenern möglich")
			}
		}
	}
	a, e := app.New(c)
	if e != nil {
		log.Fatal(e)
	}
	defer a.Store.DB.Close()
	endpoints := []struct {
		addr    string
		handler http.Handler
	}{{c.AdminAddr, a.AdminHandler()}, {c.APIAddr, a.APIHandler()}}
	if *demo {
		endpoints = append(endpoints, struct {
			addr    string
			handler http.Handler
		}{"127.0.0.1:19091", a.DemoHandler()})
	}
	servers := []*http.Server{}
	listeners := []net.Listener{}
	for _, ep := range endpoints {
		ln, e := net.Listen("tcp", ep.addr)
		if e != nil {
			for _, l := range listeners {
				_ = l.Close()
			}
			log.Fatalf("Adresse %s nicht verfügbar: %v", ep.addr, e)
		}
		listeners = append(listeners, ln)
		servers = append(servers, &http.Server{Handler: ep.handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 * 1024})
	}
	for i, s := range servers {
		go func(s *http.Server, l net.Listener) {
			if e := s.Serve(l); e != nil && e != http.ErrServerClosed {
				log.Printf("Listener beendet: %v", e)
			}
		}(s, listeners[i])
	}
	log.Printf("Schleuse · Lokaler Pilot\nVerwaltung: %s\nClient-API: %s", c.AdminOrigin, c.APIAddr)
	if *demo {
		log.Print("Demo: ausschließlich Dummy-Daten. Login demo / Lokal-testen-2026!")
	} else {
		log.Print("Erststart: Einrichtungscode im Datenordner (setup-code).")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdown)
	}
}
