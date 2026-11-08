package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"gopkg.in/ini.v1"
)

// AppConfig menyimpan state aplikasi di memori agar cepat diakses
type AppConfig struct {
	sync.RWMutex
	APIKey         string
	TargetRedirect string
	IniFile        *ini.File
}

var config AppConfig

func initConfig() {
	cfg, err := ini.Load("settings.ini")
	if err != nil {
		log.Fatalf("Gagal membaca settings.ini: %v", err)
	}

	config.IniFile = cfg
	config.APIKey = cfg.Section("").Key("APIKEY").String()
	config.TargetRedirect = cfg.Section("").Key("TARGET_REDIRECT").String()
}

// saveConfig menyimpan perubahan URL kembali ke file settings.ini
func saveConfig(newURL string) error {
	config.IniFile.Section("").Key("TARGET_REDIRECT").SetValue(newURL)
	return config.IniFile.SaveTo("settings.ini")
}

// handlerRedirect memproses endpoint /go
func handlerRedirect(w http.ResponseWriter, r *http.Request) {
	config.RLock()
	targetURL := config.TargetRedirect
	config.RUnlock()

	if targetURL == "" {
		http.Error(w, "Target redirect belum dikonfigurasi", http.StatusNotFound)
		return
	}

	// Menggunakan 302 Found (Temporary Redirect)
	http.Redirect(w, r, targetURL, http.StatusFound)
}

// handlerEdit memproses endpoint /api/edit
func handlerEdit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Menerima parameter dari URL Query String (GET) maupun Form/Body (POST)
	apikey := r.FormValue("apikey")
	newURL := r.FormValue("url")

	// Validasi API Key
	config.RLock()
	validKey := config.APIKey
	config.RUnlock()

	if apikey != validKey {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized: API Key salah atau tidak ada"})
		return
	}

	// Validasi input URL
	if newURL == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Parameter 'url' wajib diisi"})
		return
	}

	// Update memory dan simpan ke file secara thread-safe
	config.Lock()
	defer config.Unlock()

	config.TargetRedirect = newURL
	err := saveConfig(newURL)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Gagal menyimpan ke settings.ini: %v", err)})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message":         "Target redirect berhasil diperbarui",
		"target_redirect": newURL,
	})
}

func main() {
	// Inisialisasi konfigurasi saat start
	initConfig()

	mux := http.NewServeMux()

	// Routing endpoints
	mux.HandleFunc("/go", handlerRedirect)
	mux.HandleFunc("/api/edit", handlerEdit)

	port := ":8080"
	log.Printf("Server berjalan di port %s", port)

	// Start server
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
