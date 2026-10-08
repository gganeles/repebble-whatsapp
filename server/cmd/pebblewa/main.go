// pebblewa is the WhatsApp bridge for the Repebble WhatsApp watchapp. It runs in
// Termux on the phone, links to WhatsApp as a companion device, and serves a
// small localhost API for PebbleKit JS.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/repebble/repebble-whatsapp/server/internal/api"
	"github.com/repebble/repebble-whatsapp/server/internal/hub"
	"github.com/repebble/repebble-whatsapp/server/internal/store"
	"github.com/repebble/repebble-whatsapp/server/internal/wa"
	"github.com/repebble/repebble-whatsapp/server/internal/watchtext"
)

func main() {
	home, _ := os.UserHomeDir()
	dataDir := flag.String("data", filepath.Join(home, ".local", "share", "pebblewa"), "data directory")
	addr := flag.String("addr", "127.0.0.1:8723", "listen address (keep it on localhost)")
	pairPhone := flag.String("pair", "", "phone number to link with a pairing code, e.g. +491512345678")
	keepEmoji := flag.Bool("keep-emoji", false, "send emoji to the watch unchanged instead of replacing them")
	logLevel := flag.String("log", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: pebblewa [flags]\n       pebblewa token    print the API token\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		fatal("create data dir: %v", err)
	}
	token, err := loadToken(filepath.Join(*dataDir, "token"))
	if err != nil {
		fatal("token: %v", err)
	}
	if flag.Arg(0) == "token" {
		fmt.Println(token)
		return
	}

	log := waLog.Stdout("pebblewa", strings.ToUpper(*logLevel), true)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, filepath.Join(*dataDir, "app.db"))
	if err != nil {
		fatal("open store: %v", err)
	}
	defer st.Close()

	h := hub.New()
	client, err := wa.New(ctx, filepath.Join(*dataDir, "wa.db"), st, h, log)
	if err != nil {
		fatal("open whatsapp store: %v", err)
	}
	if err := client.Start(ctx); err != nil {
		fatal("connect: %v", err)
	}
	defer client.Disconnect()

	if *pairPhone != "" && client.Status().State == wa.StateUnpaired {
		go func() {
			code, err := client.Pair(ctx, *pairPhone)
			if err != nil {
				log.Errorf("Pairing failed: %v", err)
				return
			}
			fmt.Printf("\n  Pairing code: %s\n\n  On your phone: WhatsApp > Linked devices > Link a device >\n  Link with phone number instead, then enter the code.\n\n", code)
		}()
	} else if client.Status().State == wa.StateUnpaired {
		fmt.Println("\n  Not linked yet. Restart with -pair +<your number>, or pair from the watch.")
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.New(client, st, h, token, watchtext.Options{KeepEmoji: *keepEmoji}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	fmt.Printf("  API on http://%s  (token: %s)\n", *addr, token)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("listen: %v", err)
	}
}

func loadToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	t := hex.EncodeToString(buf)
	return t, os.WriteFile(path, []byte(t+"\n"), 0o600)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pebblewa: "+format+"\n", args...)
	os.Exit(1)
}
