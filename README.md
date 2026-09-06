# Meme-r

A small, single-user meme store. Go serves the UI and original image files from one binary. No database or JavaScript dependencies.

## Run

With Go 1.26.1 or later:

```sh
go build -o memer-app .
./memer-app
```

Open `http://localhost:6942`.

Optional settings:

```sh
./memer-app -addr 127.0.0.1:6942 -data-dir /path/to/meme-data -max-upload-bytes 10485760
```

- The default address is `:6942` (all interfaces). There is no authentication. Keep access private on esquie or bind to loopback behind your existing private proxy.
- The data directory defaults to the current working directory. It contains `memes.json` and `images/`. Keep this directory persistent when replacing the binary.
- Run only **one process per data directory**. Uploads within that process are serialized. Metadata updates use a temporary file and atomic rename; this is not a multi-process database or a substitute for backups.
- To back up the collection, stop the app and copy both `memes.json` and `images/` together.
- The default **whole request** limit is 10 MiB, including multipart fields and headers. A proxy can impose a smaller limit.

## Docker on esquie

The image is `ghcr.io/regutierrez/meme-r:latest`. It supports Linux AMD64 and ARM64. It contains only the app and its embedded UI, not your memes or metadata.

### Publish to GHCR

Push these files to `main` on GitHub. The `Test and publish container image` workflow runs the tests, builds both architectures, and publishes:

- `latest`: the latest successful build from `main`.
- `sha-<full-commit-sha>`: a commit-specific tag for rollback.

Pull requests build and test without publishing. You can also run the workflow manually on `main`. Publishing uses the repository's automatic `GITHUB_TOKEN`; no registry secret is needed.

GHCR packages start private. To keep the image private, sign in once on esquie:

```sh
docker login ghcr.io -u regutierrez
```

Use a GitHub personal access token (classic) with `read:packages` as the password. Do not put the token in Compose or commit it. Alternatively, you can make the package public in GitHub's package settings to allow anonymous pulls. Image visibility does not expose the data volume or make the running app public.

### Start and update

Copy `compose.yaml` to a fixed directory on esquie, then run from that directory:

```sh
docker compose pull
docker compose up -d
```

Run the same two commands after publishing changes. **Pulling alone does not update a running container**; `up -d` replaces it while keeping the volume.

For a direct pull:

```sh
docker pull ghcr.io/regutierrez/meme-r:latest
```

Compose stores metadata and original files in the named volume `meme-r_memes`, mounted at `/data`. The process runs as UID/GID `10001:10001`. Docker initializes a new named volume with the correct directory ownership. Keep one instance, and do not use `docker compose down -v`: that deletes the collection.

No restart policy or extra service is configured. After a host restart, start the app with `docker compose up -d`.

### Private access

The port binds to `127.0.0.1:6942` on esquie by default. A host-based proxy can reach it there. For a proxy in another container, attach the app to your existing proxy network and route to `meme-r:6942`; localhost inside that proxy is not the host.

For direct access through a private host interface, set `MEME_BIND_ADDRESS` to that interface's IP in a `.env` file beside Compose. `MEME_PORT` changes the host port. Do not expose this unauthenticated app to the public internet. Use HTTPS through your existing proxy for browser clipboard access.

### Rollback and local builds

Set `MEME_IMAGE=ghcr.io/regutierrez/meme-r:sha-<full-commit-sha>` in `.env`, then run `docker compose pull` and `docker compose up -d`. Remove that setting to follow `latest` again.

To test a local build without GHCR:

```sh
docker build -t meme-r:local .
MEME_IMAGE=meme-r:local docker compose up -d
```

If you already have a collection, copy both `memes.json` and `images/` into the volume **before starting the app**, and make them writable by UID/GID `10001:10001`. The image does not import files from the source checkout. Back up both together while the app is stopped.

## Use

Upload with the button, drag and drop, or paste an image. Names are required and must be unique without regard to case. Tags are optional.

GIF, PNG, JPEG, and WebP content signatures are accepted. The app stores original bytes, not converted thumbnails. Animated GIF files animate in the gallery, upload preview, and clicked preview. Pasting from another app can supply a still image instead of the original GIF; use the actual file when animation matters.

Search matches names or tags. Selected tag filters match **any** selected tag. Search and tag filters apply together. The gallery scrolls, with newest-first, oldest-first, and name sorting.

Open a meme to download the original or copy it to the clipboard. Clipboard image support depends on the browser, requires a secure context (HTTPS or localhost), and can also depend on the receiving app. The app reports unsupported formats rather than silently flattening GIFs. Use **download original** when copying is unavailable. There is no copy-link feature.

Keyboard: `a` opens upload, `/` focuses search, and Escape closes dialogs. Cards work with Tab and Enter or Space.

## Checks

```sh
go test -race ./...
go vet ./...
node --check static/meme-gallery.js
```

HTTP tests cover animated GIF byte/frame preservation, content-based extensions, request limits, rejected uploads, empty storage, concurrent uploads, duplicate names, and static routes.

Manual browser checks:

1. Upload an animated `.gif` file. Check motion in the upload preview, gallery, and clicked preview. Download it and confirm it still animates.
2. Try clipboard copy over HTTPS. Confirm either a successful paste or a visible unsupported-format message. GIFs must not silently become still images.
3. Upload a name and tags containing quotes and `<b>text</b>`. Confirm they appear as text, not markup.
4. Check search, multiple tag filters, every sort option, and an empty result.
5. Check a collection larger than 12 images, a narrow phone window, and keyboard-only navigation.
