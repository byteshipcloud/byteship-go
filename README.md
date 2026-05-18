# Byteship Go SDK

Go client for the Byteship upload API.

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/byteshipcloud/byteship-go"
)

var client = byteship.NewClient(
	byteship.WithAPIKey(os.Getenv("BYTESHIP_API_KEY")),
)

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	uploaded, err := client.Upload(r.Context(), byteship.UploadInput{
		Reader:      file,
		Filename:    header.Filename,
		ContentType: header.Header.Get("Content-Type"),
		Path:        "uploads/" + header.Filename,
		Visibility:  byteship.VisibilityPublic,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"file": uploaded,
	})
}

func main() {
	http.HandleFunc("/uploads", uploadHandler)
	log.Fatal(http.ListenAndServe(":3000", nil))
}
```

## Multipart Uploads

Large uploads automatically use multipart sessions when the API selects them. You can force multipart uploads and tune concurrency when you need resumable part recording:

```go
file, err := os.Open("video.mp4")
if err != nil {
	log.Fatal(err)
}
defer file.Close()

uploaded, err := client.Upload(context.Background(), byteship.UploadInput{
	Reader:               file,
	Filename:             "video.mp4",
	ContentType:          "video/mp4",
	Path:                 "uploads/video.mp4",
	Method:               byteship.UploadMethodMultipart,
	MultipartConcurrency: 4,
})
```

When `Upload` uses multipart, `UploadInput.Reader` must implement `io.ReaderAt` so the SDK can upload part ranges independently. Files from `os.Open` and `r.FormFile` satisfy this; for non-seekable streams, set `Method: byteship.UploadMethodSingle`.

## Image URLs

```go
width := 800
src, err := byteship.ImageURL(*uploaded.URL, byteship.ImageTransform{
	Width:  &width,
	Format: "webp",
})
if err != nil {
	log.Fatal(err)
}

srcset, err := byteship.ImageSrcSet(*uploaded.URL, byteship.ResponsiveImageOptions{
	Widths: []int{320, 640, 960},
	Transform: byteship.ImageTransform{
		Format: "webp",
		Fit:    "cover",
	},
})
```

## Features

- `Upload` creates an upload session, streams bytes to storage, and completes it, including multipart uploads when selected.
- `UploadMany` uploads batches with bounded concurrency.
- `CreateUploadToken` mints scoped browser upload tokens from trusted server code.
- `CreateFileUpload`, `CreateUpload`, `CreateUploadPartURLs`, `CompletePathUpload`, and `CompleteUpload` expose the lower-level upload lifecycle.
- `GetFile`, `CreateSignedURL`, and `DeleteFile` cover file management and private delivery.
- `ImageURL` and `ImageSrcSet` build Byteship `tr` transform URLs for responsive images.
- `WithUploadToken`, `WithBaseURL`, and `WithHTTPClient` support browser-token flows, tests, and custom transports.
- `AsError` unwraps structured Byteship API errors with `Code`, `StatusCode`, and response `Details`.
