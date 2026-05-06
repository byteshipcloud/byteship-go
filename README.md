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

## Features

- `Upload` creates an upload session, streams bytes to storage, and completes it.
- `UploadMany` uploads batches with bounded concurrency.
- `CreateUploadToken` mints scoped browser upload tokens from trusted server code.
- `CreateFileUpload`, `CreateUpload`, and `CompleteUpload` expose the lower-level upload lifecycle.
- `GetFile`, `CreateSignedURL`, and `DeleteFile` cover file management and private delivery.
- `WithUploadToken`, `WithBaseURL`, and `WithHTTPClient` support browser-token flows, tests, and custom transports.
- `AsError` unwraps structured Byteship API errors with `Code`, `StatusCode`, and response `Details`.
