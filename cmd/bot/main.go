
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Armin-kho/persian-currency-bot/internal/bot"
	"github.com/Armin-kho/persian-currency-bot/internal/config"
)

func main() {
	cfgPath := flag.String("config", config.DefaultConfigPath(), "path to config.json")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	app, err := bot.New(cfg)
	if err != nil {
		log.Fatalf("init error: %v", err)
	}
	defer app.Close()

	// Graceful stop
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("Shutting down...")
		app.Close()
		os.Exit(0)
	}()

	if err := app.Run(); err != nil {
		log.Fatalf("run error: %v", err)
	}
}
