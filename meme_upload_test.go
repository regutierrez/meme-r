package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testMemeStore(t *testing.T) (*memeStore, http.Handler) {
	t.Helper()
	store := &memeStore{dir: t.TempDir(), maxUploadBytes: 10 * 1024 * 1024}
	if err := os.Mkdir(filepath.Join(store.dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	return store, store.routes()
}

func animatedMemeGIF(t *testing.T) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second.SetColorIndex(0, 0, 1)
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 10}, LoopCount: 0}); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func memeUploadRequest(t *testing.T, name, tags, filename string, imageData []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("name", name); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("tags", tags); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(imageData); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/memes", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	return request
}

func listTestMemes(t *testing.T, handler http.Handler) []Meme {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/memes", nil))
	if response.Code != 200 {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	var memes []Meme
	if err := json.Unmarshal(response.Body.Bytes(), &memes); err != nil {
		t.Fatal(err)
	}
	if memes == nil {
		t.Fatal("list must return an array, not null")
	}
	return memes
}

func TestMemeAnimatedGIFRoundTrip(t *testing.T) {
	_, handler := testMemeStore(t)
	original := animatedMemeGIF(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, memeUploadRequest(t, " animated ", " fun, fun, ", "wrong.html", original))
	if response.Code != 201 {
		t.Fatalf("upload: %d %s", response.Code, response.Body.String())
	}
	memes := listTestMemes(t, handler)
	if len(memes) != 1 || memes[0].Name != "animated" || len(memes[0].Tags) != 1 {
		t.Fatalf("unexpected metadata: %+v", memes)
	}
	if filepath.Ext(memes[0].ImageURL) != ".gif" {
		t.Fatal("extension must come from content")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", memes[0].ImageURL, nil))
	if response.Code != 200 || response.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("image response: %d %v", response.Code, response.Header())
	}
	if !bytes.Equal(original, response.Body.Bytes()) {
		t.Fatal("original GIF bytes changed")
	}
	animation, err := gif.DecodeAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(animation.Image) != 2 || animation.LoopCount != 0 {
		t.Fatal("animation frames or looping lost")
	}
}

func TestMemeEmptyStorage(t *testing.T) {
	for _, content := range []string{"", " \n", "null", "[]"} {
		t.Run(fmt.Sprintf("content-%q", content), func(t *testing.T) {
			store, handler := testMemeStore(t)
			if got := listTestMemes(t, handler); len(got) != 0 {
				t.Fatal(got)
			}
			if err := os.WriteFile(filepath.Join(store.dir, "memes.json"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := listTestMemes(t, handler); len(got) != 0 {
				t.Fatal(got)
			}
		})
	}
}

func TestMemeUploadValidation(t *testing.T) {
	for _, test := range []struct {
		name, title string
		data        []byte
		limit       int64
		status      int
	}{
		{"blank name", "  ", animatedMemeGIF(t), 10000, 400},
		{"HTML", "html", []byte("<!doctype html><script>alert(1)</script>"), 10000, 415},
		{"SVG", "svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 10000, 415},
		{"empty image", "empty", nil, 10000, 415},
		{"size limit", "big", animatedMemeGIF(t), 32, 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, handler := testMemeStore(t)
			store.maxUploadBytes = test.limit
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, memeUploadRequest(t, test.title, "", "test.gif", test.data))
			if response.Code != test.status {
				t.Fatalf("got %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			files, err := os.ReadDir(filepath.Join(store.dir, "images"))
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 || len(listTestMemes(t, handler)) != 0 {
				t.Fatal("rejected upload changed storage")
			}
		})
	}
}

func TestMemeConcurrentUploads(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate-%t", duplicate), func(t *testing.T) {
			store, handler := testMemeStore(t)
			data := animatedMemeGIF(t)
			const count = 20
			requests := make([]*http.Request, count)
			for i := range requests {
				name := fmt.Sprintf("meme-%d", i)
				if duplicate {
					name = "same"
					if i%2 == 0 {
						name = " SAME "
					}
				}
				requests[i] = memeUploadRequest(t, name, "", "test.gif", data)
			}
			responses := make(chan int, count)
			var workers sync.WaitGroup
			for _, request := range requests {
				workers.Go(func() {
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, request)
					responses <- response.Code
				})
			}
			workers.Wait()
			close(responses)
			created := 0
			for status := range responses {
				if status == 201 {
					created++
				} else if !duplicate || status != 409 {
					t.Fatalf("unexpected status %d", status)
				}
			}
			want := count
			if duplicate {
				want = 1
			}
			if created != want {
				t.Fatalf("created %d, want %d", created, want)
			}
			memes := listTestMemes(t, handler)
			if len(memes) != want {
				t.Fatalf("stored %d, want %d", len(memes), want)
			}
			for _, meme := range memes {
				if meme.Tags == nil {
					t.Fatal("tags must be an array")
				}
			}
			files, err := os.ReadDir(filepath.Join(store.dir, "images"))
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != want {
				t.Fatalf("image count %d, want %d", len(files), want)
			}
		})
	}
}

func TestMemeCorruptMetadataIsPreserved(t *testing.T) {
	store, handler := testMemeStore(t)
	path := filepath.Join(store.dir, "memes.json")
	original := []byte("broken metadata")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, memeUploadRequest(t, "new", "", "test.gif", animatedMemeGIF(t)))
	if response.Code != 500 {
		t.Fatalf("got %d", response.Code)
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, actual) {
		t.Fatal("corrupt metadata overwritten")
	}
}

func TestMemeRoutes(t *testing.T) {
	_, handler := testMemeStore(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, test := range []struct {
		path     string
		status   int
		contains string
	}{
		{"/", 200, "download original"},
		{"/static/meme-gallery.js", 200, "function renderMemeCard"},
		{"/missing", 404, "404"},
	} {
		response, err := http.Get(server.URL + test.path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != test.status || !bytes.Contains(body, []byte(test.contains)) {
			t.Fatalf("route %s: %d %s", test.path, response.StatusCode, body)
		}
		if response.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("missing nosniff")
		}
	}
}
