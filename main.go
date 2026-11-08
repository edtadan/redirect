package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"gopkg.in/ini.v1"
)

// AppConfig menyimpan state konfigurasi di memory
type AppConfig struct {
	APIKey         string
	TargetRedirect string
	mu             sync.RWMutex
}

var config AppConfig

const configFile = "settings.ini"

// loadConfig membaca target dari file INI dan API Key dari Environment Variable
func loadConfig() {
	// Load API Key dari env (dieksekusi sekali saat start)
	config.APIKey = os.Getenv("API_KEY")
	if config.APIKey == "" {
		log.Fatal("Environment variable API_KEY belum di-set!")
	}

	// Load Target Redirect dari settings.ini
	cfg, err := ini.Load(configFile)
	if err != nil {
		log.Fatalf("Gagal membaca %s: %v", configFile, err)
	}

	config.mu.Lock()
	defer config.mu.Unlock()
	config.TargetRedirect = cfg.Section("").Key("TARGET_REDIRECT").String()
}

// updateConfig mengubah state di memory dan menyimpannya kembali ke settings.ini
func updateConfig(newURL string) error {
	cfg, err := ini.Load(configFile)
	if err != nil {
		return err
	}

	// Update memory (Thread-Safe)
	config.mu.Lock()
	config.TargetRedirect = newURL
	config.mu.Unlock()

	// Update file INI
	cfg.Section("").Key("TARGET_REDIRECT").SetValue(newURL)
	err = cfg.SaveTo(configFile)
	if err != nil {
		return err
	}

	return nil
}

// redirectHandler menangani endpoint /go
func redirectHandler(w http.ResponseWriter, r *http.Request) {
	config.mu.RLock()
	targetURL := config.TargetRedirect
	config.mu.RUnlock()

	if targetURL == "" {
		http.Error(w, "Target URL belum dikonfigurasi", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, targetURL, http.StatusFound)
}

// editHandler menangani endpoint /api/edit
func editHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	apikey := r.URL.Query().Get("apikey")
	newURL := r.URL.Query().Get("url")

	if apikey == "" || newURL == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Parameter 'apikey' dan 'url' wajib diisi"})
		return
	}

	// Karena API Key dari env bersifat statis selama runtime, kita tidak perlu RLock untuk membacanya
	if apikey != config.APIKey {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "API Key tidak valid"})
		return
	}

	err := updateConfig(newURL)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Gagal menyimpan konfigurasi: " + err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Target redirect berhasil diupdate",
		"new_url": newURL,
	})
}

func main() {
	loadConfig()

	mux := http.NewServeMux()
	mux.HandleFunc("/go", redirectHandler)
	mux.HandleFunc("/api/edit", editHandler)

	port := ":8080"
	fmt.Printf("Server berjalan di port %s...\n", port)

	err := http.ListenAndServe(port, mux)
	if err != nil {
		log.Fatalf("Server gagal berjalan: %v", err)
	}
}
