package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/ini.v1"
)

// AppConfig menyimpan state konfigurasi di memory
type AppConfig struct {
	APIKey         string
	TargetRedirect string
	BlockRedirect  string
	mu             sync.RWMutex
}

var config AppConfig

const configFile = "settings.ini"

// loadConfig membaca target dari file INI dan API Key dari Environment Variable
func loadConfig() {
	config.APIKey = os.Getenv("API_KEY")
	if config.APIKey == "" {
		log.Fatal("Environment variable API_KEY belum di-set!")
	}

	cfg, err := ini.Load(configFile)
	if err != nil {
		log.Fatalf("Gagal membaca %s: %v", configFile, err)
	}

	config.mu.Lock()
	defer config.mu.Unlock()
	config.TargetRedirect = cfg.Section("").Key("TARGET_REDIRECT").String()
	config.BlockRedirect = cfg.Section("").Key("BLOCK_REDIRECT").String()
}

// updateConfig mengubah state di memory dan menyimpannya kembali ke settings.ini
func updateConfig(newURL string, newBlockURL string) error {
	cfg, err := ini.Load(configFile)
	if err != nil {
		return err
	}

	config.mu.Lock()
	if newURL != "" {
		config.TargetRedirect = newURL
		cfg.Section("").Key("TARGET_REDIRECT").SetValue(newURL)
	}
	if newBlockURL != "" {
		config.BlockRedirect = newBlockURL
		cfg.Section("").Key("BLOCK_REDIRECT").SetValue(newBlockURL)
	}
	config.mu.Unlock()

	return cfg.SaveTo(configFile)
}

// getClientIP mendapatkan IP asli client (mendukung jika ada di balik Nginx/Cloudflare)
func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		ips := strings.Split(ip, ",")
		return strings.TrimSpace(ips[0])
	}
	ip = r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// checkBlackbox mengirim request ke API blackbox dengan timeout 10 detik
func checkBlackbox(ip string) bool {
	// Timeout setara dengan AbortSignal.timeout(10000)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	apiURL := fmt.Sprintf("https://blackbox.ipinfo.app/lookup/%s", url.PathEscape(ip))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	result := strings.TrimSpace(string(bodyBytes))
	return result == "Y"
}

// redirectHandler menangani endpoint /go
func redirectHandler(w http.ResponseWriter, r *http.Request) {
	config.mu.RLock()
	targetURL := config.TargetRedirect
	blockURL := config.BlockRedirect
	config.mu.RUnlock()

	if targetURL == "" {
		http.Error(w, "Target URL belum dikonfigurasi", http.StatusInternalServerError)
		return
	}

	ip := getClientIP(r)
	isBlocked := checkBlackbox(ip)

	// Tentukan base URL berdasarkan status block
	finalURL := targetURL
	if isBlocked && blockURL != "" {
		finalURL = blockURL
		log.Printf("[BLOCK] HTTP trigger function processing request for IP filter. %s\n", ip)
	} else {
		log.Printf("[PASS] HTTP trigger function processing request for IP filter. %s\n", ip)
	}

	// Teruskan searchParams (query string) jika ada
	searchParams := r.URL.RawQuery
	if searchParams != "" {
		if strings.Contains(finalURL, "?") {
			finalURL = finalURL + "&" + searchParams
		} else {
			finalURL = finalURL + "?" + searchParams
		}
	}

	http.Redirect(w, r, finalURL, http.StatusFound)
}

// editHandler menangani endpoint /api/edit
func editHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	apikey := r.URL.Query().Get("apikey")
	newURL := r.URL.Query().Get("url")
	newBlockURL := r.URL.Query().Get("block_url") // Opsional jika ingin update block link juga

	if apikey == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Parameter 'apikey' wajib diisi"})
		return
	}

	config.mu.RLock()
	validKey := config.APIKey
	config.mu.RUnlock()

	if apikey != validKey {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "API Key tidak valid"})
		return
	}

	err := updateConfig(newURL, newBlockURL)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Gagal menyimpan konfigurasi: " + err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message":       "Konfigurasi redirect berhasil diupdate",
		"new_url":       newURL,
		"new_block_url": newBlockURL,
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
