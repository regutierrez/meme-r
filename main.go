package main

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var staticFS embed.FS

// Meme stores metadata; ImageURL refers to the unchanged uploaded file.
type Meme struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Tags     []string `json:"tags"`
	ImageURL string   `json:"image_url"`
	AddedAt  string   `json:"added_at"`
}

type memeStore struct {
	dir            string
	maxUploadBytes int64
	mu             sync.Mutex
}

func main() {
	addr := flag.String("addr", ":6942", "HTTP listen address")
	dir := flag.String("data-dir", ".", "Persistent directory for memes.json and images")
	maxUpload := flag.Int64("max-upload-bytes", 10*1024*1024, "Maximum multipart request size in bytes")
	flag.Parse()
	if *maxUpload <= 0 {
		log.Fatal("meme upload limit must be positive")
	}
	store := &memeStore{dir: *dir, maxUploadBytes: *maxUpload}
	if err := os.MkdirAll(filepath.Join(*dir, "images"), 0o755); err != nil {
		log.Fatal(err)
	}
	log.Printf("meme server listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, store.routes()))
}

func (s *memeStore) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /images/", http.StripPrefix("/images/", http.FileServer(http.Dir(filepath.Join(s.dir, "images")))))
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("GET /api/memes", s.handleListMemes)
	mux.HandleFunc("POST /api/memes", s.handleCreateMeme)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index html file not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *memeStore) readMemes() ([]Meme, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "memes.json"))
	memes := []Meme{}
	if errors.Is(err, os.ErrNotExist) {
		return memes, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) != 0 {
		if err := json.Unmarshal(data, &memes); err != nil {
			return nil, err
		}
	}
	if memes == nil {
		memes = []Meme{}
	}
	for i := range memes {
		if memes[i].Tags == nil {
			memes[i].Tags = []string{}
		}
	}
	return memes, nil
}

func (s *memeStore) handleListMemes(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	memes, err := s.readMemes()
	s.mu.Unlock()
	if err != nil {
		log.Printf("meme list failed: %v", err)
		http.Error(w, "meme list failed: storage error", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(memes)
}

// writeMemes replaces metadata atomically. The caller holds the store lock.
func (s *memeStore) writeMemes(memes []Meme) error {
	file, err := os.CreateTemp(s.dir, ".memes-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := json.NewEncoder(file).Encode(memes); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(s.dir, "memes.json"))
}

func (s *memeStore) handleCreateMeme(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadBytes)
	if err := r.ParseMultipartForm(s.maxUploadBytes); err != nil {
		var limitError *http.MaxBytesError
		if errors.As(err, &limitError) {
			http.Error(w, "meme upload exceeds request size limit", 413)
		} else {
			http.Error(w, "meme upload: invalid multipart form", 400)
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "meme upload: select an image", 400)
		return
	}
	defer file.Close()
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "meme upload: name is required", 400)
		return
	}
	tags := []string{}
	seenTags := map[string]bool{}
	for _, rawTag := range strings.Split(r.FormValue("tags"), ",") {
		tag := strings.TrimSpace(rawTag)
		if tag != "" && !seenTags[tag] {
			tags = append(tags, tag)
			seenTags[tag] = true
		}
	}
	// Inspect content, not the supplied extension. Never re-encode animated GIFs.
	prefix := make([]byte, 512)
	n, err := io.ReadFull(file, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		http.Error(w, "meme upload: cannot read image", 400)
		return
	}
	ext := map[string]string{"image/gif": ".gif", "image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[http.DetectContentType(prefix[:n])]
	if ext == "" {
		http.Error(w, "meme upload: use GIF, PNG, JPEG, or WebP", 415)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "meme upload: cannot read image", 400)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	storageError := func(err error) {
		log.Printf("meme upload storage failed: %v", err)
		http.Error(w, "meme upload: storage error", 500)
	}
	memes, err := s.readMemes()
	if err != nil {
		storageError(err)
		return
	}
	for _, meme := range memes {
		if strings.EqualFold(meme.Name, name) {
			http.Error(w, "meme with this name already exists", 409)
			return
		}
	}
	id := rand.Text()
	diskPath := filepath.Join(s.dir, "images", id+ext)
	dst, err := os.OpenFile(diskPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		storageError(err)
		return
	}
	saved := false
	defer func() {
		_ = dst.Close()
		if !saved {
			_ = os.Remove(diskPath)
		}
	}()
	if _, err := io.Copy(dst, file); err != nil {
		storageError(err)
		return
	}
	if err := dst.Sync(); err != nil {
		storageError(err)
		return
	}
	if err := dst.Close(); err != nil {
		storageError(err)
		return
	}
	meme := Meme{ID: id, Name: name, Tags: tags, ImageURL: "/images/" + id + ext, AddedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := s.writeMemes(append(memes, meme)); err != nil {
		storageError(err)
		return
	}
	saved = true
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(meme)
}
