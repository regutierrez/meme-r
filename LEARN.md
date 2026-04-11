# meme-r: what you need to learn

you're building a go server that serves an html/css frontend, handles image uploads, and persists meme metadata to a json file on disk.

---

## 1. go basics (skip if you know go)

- variables, structs, slices, maps
- error handling (`if err != nil`)
- pointers (you'll hit `*http.Request` immediately)
- packages and imports

**resource**: https://go.dev/tour — do the basics section, skip concurrency for now

---

## 2. `net/http` — the only dependency you need

go's stdlib http server is production-grade. no frameworks needed.

### what to learn

- `http.HandleFunc(pattern, handler)` — register a route
- `http.ListenAndServe(addr, nil)` — start the server
- `http.ResponseWriter` — write responses (json, html, status codes)
- `http.Request` — read method, url path, query params, body, form data
- `http.FileServer` + `http.StripPrefix` — serve static files (your html/css/images)

### routes you'll need

```
GET  /                → serve index.html
GET  /static/         → serve css, js, images
GET  /api/memes       → return all memes as json
POST /api/memes       → add a new meme (json body or multipart form)
DELETE /api/memes/{id} → delete a meme
```

### key pattern: explicit routes first

for a tiny project, `switch r.Method` is okay. but the better default is **one small handler per method + path**.

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /api/memes", handleListMemes)
mux.HandleFunc("POST /api/memes", handleCreateMeme)
mux.HandleFunc("DELETE /api/memes/{id}", handleDeleteMeme)
```

if you do use `switch r.Method`, keep it as a thin dispatcher only.

**resource**: https://pkg.go.dev/net/http — read the package overview and examples

---

## 3. serving static files

you need to serve `index.html`, `style.css`, and the `images/` folder.

### what to learn

- `http.FileServer(http.Dir("./static"))` — serves a directory
- `http.StripPrefix("/static/", fileServer)` — maps url path to disk path
- or just read `index.html` and serve it at `/` with `http.ServeFile`

### suggested disk layout

```
meme-r/
  main.go
  memes.json
  static/
    index.html
    style.css
  images/
```

---

## 4. json in go

you'll read/write `memes.json` for persistence and send/receive json over the api.

### what to learn

- `encoding/json` package
- struct tags: `json:"name"` to control field names
- `json.Marshal(v)` — struct/slice → json bytes
- `json.Unmarshal(data, &v)` — json bytes → struct/slice
- `json.NewEncoder(w).Encode(v)` — write json to http response
- `json.NewDecoder(r.Body).Decode(&v)` — read json from http request body

### your meme struct

```go
type Meme struct {
    ID       string   `json:"id"`
    Name     string   `json:"name"`
    Tags     []string `json:"tags"`
    ImageURL string   `json:"image_url"` // url or local path
    AddedAt  string   `json:"added_at"`
}
```

---

## 5. file i/o — reading/writing `memes.json`

### what to learn

- `os.ReadFile(path)` — read entire file into `[]byte`
- `os.WriteFile(path, data, perm)` — write `[]byte` to file
- file permissions: `0644` for the json file

### the pattern

```
load:  os.ReadFile → json.Unmarshal → []Meme
save:  json.Marshal → os.WriteFile
```

you load on startup, keep `[]Meme` in memory, save after every mutation.

---

## 6. handling file uploads

when the user uploads or pastes an image, the browser sends a `multipart/form-data` request.

### what to learn

- `r.ParseMultipartForm(maxMemory)` — parse the incoming form
- `r.FormFile("fieldname")` — get the uploaded file
- `io.Copy(dst, src)` — copy file contents to disk
- `os.Create(path)` — create the destination file
- generate a unique filename (use `time.Now().UnixNano()` or a uuid)
- `os.MkdirAll("images", 0755)` — ensure the images dir exists

### the flow

```
1. parse multipart form
2. get the file from the form
3. generate a unique filename
4. create file on disk in images/
5. copy uploaded data into it
6. store the local path in the meme struct
```

---

## 7. generating ids

you need unique ids for each meme. simplest options without external deps:

- `fmt.Sprintf("%d", time.Now().UnixNano())` — timestamp-based, good enough
- `crypto/rand` — for something more robust:

```go
import "crypto/rand"
import "encoding/hex"

func newID() string {
    b := make([]byte, 8)
    rand.Read(b)
    return hex.EncodeToString(b)
}
```

---

## 8. the minimal js you'll need in the frontend

html/css can't talk to an api. you need a thin js layer for:

- **fetch memes on page load**: `fetch("/api/memes")` → render cards into the grid
- **submit the add form**: intercept form submit, `fetch("/api/memes", { method: "POST", body: formData })`
- **delete a meme**: `fetch("/api/memes/ID", { method: "DELETE" })`
- **pagination**: count the cards, show/hide in groups of 12
- **search/filter/sort**: filter the in-memory list, re-render

you can put this in a `<script>` tag at the bottom of `index.html` or a separate `app.js`.

---

## 9. clipboard (copying image on click)

this is a browser api, requires js:

```js
// fetch the image as a blob, write to clipboard
const resp = await fetch(imageUrl);
const blob = await resp.blob();
await navigator.clipboard.write([
  new ClipboardItem({ [blob.type]: blob })
]);
```

note: only works over https or localhost, and requires user gesture (click).

---

## build & run

```bash
# build
go build -o meme-r .

# run
./meme-r

# or just
go run main.go
```

then open `http://localhost:8080` in any browser on any machine.

---

## order of attack

1. get a go server serving `index.html` at `/`
2. add the `GET /api/memes` endpoint returning hardcoded json
3. wire up the frontend to fetch and render memes
4. add `POST /api/memes` with json body (url-based memes)
5. add json file persistence (load on start, save on write)
6. add file upload support
7. add delete
8. add search/filter/sort in the frontend js
9. add clipboard copy on card click
10. pagination
