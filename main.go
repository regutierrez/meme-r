package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed static/*
var staticFS embed.FS

type Meme struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Tags     []string `json:"tags"`
	ImageURL string   `json:"image_url"`
	AddedAt  string   `json:"added_at"`
}

func main() {
	// from the func definition of NewServeMux:
	// ServeMux is an HTTP request multiplexer.
	// It matches the URL of each incoming request against a list of registered
	// patterns and calls the handler for the pattern that
	// most closely matches the URL.
	mux := http.NewServeMux()

	// weird that we need two handlers here just to serve the website (we're not talking about the api stuff here).
	// the reason being is they serve separate purposes:
	// mux.Handle is usually used to serve an object (in our cases stuff from the fileserver we created, which is the static dir)
	// this allows index.html to easily grab the stylesheet via `/static/style.css`.
	//
	// mux.HandleFunc on the other hand is used if we want a request to run a function. in our case, we run `handleIndex`,
	// which just basically serve index.html. This allows the html file to be served via http://<url>/
	// instead of http://<url>/index.html
	mux.Handle("GET /images/", http.StripPrefix("/images/", http.FileServer(http.Dir("images"))))
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /api/memes", handleListMemes)
	mux.HandleFunc("POST /api/memes", handleCreateMeme)

	if err := http.ListenAndServe(":6942", mux); err != nil {
		// TODO: probably better logging. Fatalf?
		panic(err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	// we wrap this inside an anonymous function because if we do this:
	// mux.HandleFunc("GET /", http.ServeFile(http.ResponseWriter, *http.Request, "static/index.html"))
	// we call the ServeFile immediately.
	// also, the HandleFunc expects a function with exactly this shape:
	// func(w http.ResponseWriter, r *http.Request)
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index html file not found", http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write(data)
}

func handleListMemes(w http.ResponseWriter, _ *http.Request) {
	data, err := os.ReadFile("memes.json")
	if err != nil {
		if os.IsNotExist(err) {
			// TODO: verify if we really don't need to handle errors here since if this fails
			// it's an internet issue
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, "grab memes failed: db error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func handleCreateMeme(w http.ResponseWriter, r *http.Request) {
	maxMem := 10 * 1024 * 1024 // 10MB

	if err := r.ParseMultipartForm(int64(maxMem)); err != nil {
		log.Printf("handleCreateMeme Error: %v", err)
		http.Error(w, "failed to ingest data", http.StatusBadRequest)
		return
	}
	// apparently files are handled very differently, so we have to do this.
	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("handleCreateMeme Error: %v", err)
		http.Error(w, "error retrieving file", http.StatusBadRequest)
		return
	}
	//nolint:errcheck
	defer file.Close()

	name, rawTags := r.FormValue("name"), strings.Split(r.FormValue("tags"), ",")
	var tags []string
	for _, t := range rawTags {
		trimmed := strings.TrimSpace(t)
		if trimmed != "" {
			tags = append(tags, trimmed)
		}
	}

	jsonData, err := os.ReadFile("memes.json")
	if err != nil {
		if os.IsNotExist(err) {
			jsonData = []byte("[]")
		} else {
			log.Printf("handleCreateMeme Error: %v", err)
			http.Error(w, "create meme failed: db error", http.StatusInternalServerError)
			return
		}
	} else if len(jsonData) == 0 {
		// we can't unmarshal shit if there's nothing in memes.json.
		// that's why we have to initialize jsonData
		jsonData = []byte("[]")
	}

	var memes []Meme

	if err := json.Unmarshal(jsonData, &memes); err != nil {
		log.Printf("handleCreateMeme Error: %v", err)
		http.Error(w, "create meme failed: db error", http.StatusInternalServerError)
		return
	}

	for _, m := range memes {
		if strings.EqualFold(m.Name, name) {
			http.Error(w, "meme with this name already exists", http.StatusConflict)
			return
		}
	}

	origFileName := filepath.Base(header.Filename)
	diskPath := filepath.Join("images", origFileName)
	publicURL := "/images/" + origFileName

	dst, err := os.Create(diskPath)
	if err != nil {
		log.Printf("handleCreateMeme Error: %v", err)
		http.Error(w, "create meme failed: db error", http.StatusInternalServerError)
		return
	}

	_, err = io.Copy(dst, file)
	if err != nil {
		log.Printf("handleCreateMeme Error: %v", err)
		http.Error(w, "create meme failed: db error", http.StatusInternalServerError)
		return
	}
	if err = dst.Close(); err != nil {
		log.Printf("handleCreateMeme Error: %v", err)
		http.Error(w, "create meme failed: db error", http.StatusInternalServerError)
		return
	}
	timeNow := time.Now()
	id := fmt.Sprintf("%d", timeNow.UnixNano())
	newMeme := Meme{
		ID:       id,
		Name:     name,
		Tags:     tags,
		ImageURL: publicURL,
		AddedAt:  timeNow.Format(time.RFC3339),
	}

	memes = append(memes, newMeme)
	updatedData, err := json.Marshal(memes)
	if err != nil {
		log.Printf("handleCreateMeme Error: %v", err)
		http.Error(w, "create meme failed: db error", http.StatusInternalServerError)
		return
	}

	if err = os.WriteFile("memes.json", updatedData, 0o644); err != nil {
		log.Printf("handleCreateMeme Error: %v", err)
		http.Error(w, "create meme failed: db error", http.StatusInternalServerError)
		return
	}

	log.Printf("Meme created! Name: %s, Tags: %s\n", name, tags)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
