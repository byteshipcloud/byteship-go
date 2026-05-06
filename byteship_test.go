package byteship

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUploadCreatesPutsAndCompletes(t *testing.T) {
	t.Parallel()

	var sawCreate atomic.Bool
	var sawPut atomic.Bool
	var sawComplete atomic.Bool

	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("storage method = %s, want PUT", r.Method)
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("content-type"); got != "text/plain" {
			t.Errorf("storage content-type = %q, want text/plain", got)
			http.Error(w, "wrong content-type", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		if string(body) != "hello byteship" {
			t.Errorf("storage body = %q", string(body))
			http.Error(w, "wrong body", http.StatusBadRequest)
			return
		}
		sawPut.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer storage.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer bs_test_key" {
			t.Errorf("authorization = %q", got)
			http.Error(w, "wrong authorization", http.StatusUnauthorized)
			return
		}

		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/files/uploads/note.txt":
			var input CreateFileUploadInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				http.Error(w, "decode failed", http.StatusBadRequest)
				return
			}
			if input.ByteSize != int64(len("hello byteship")) {
				t.Errorf("byteSize = %d", input.ByteSize)
				http.Error(w, "wrong byte size", http.StatusBadRequest)
				return
			}
			if input.Visibility != VisibilityPublic {
				t.Errorf("visibility = %q", input.Visibility)
				http.Error(w, "wrong visibility", http.StatusBadRequest)
				return
			}
			sawCreate.Store(true)
			writeJSON(t, w, http.StatusCreated, map[string]any{
				"file": map[string]any{
					"id":     "file_123",
					"path":   "uploads/note.txt",
					"status": "pending",
					"url":    "https://cdn.byteship.cloud/f/p_x7K9mQ/uploads/note.txt",
				},
				"upload": map[string]any{
					"expiresAt": "2026-05-06T12:00:00Z",
					"fileId":    "file_123",
					"headers": map[string]string{
						"content-type": "text/plain",
					},
					"id":     "upload_123",
					"key":    "project/uploads/file_123-note.txt",
					"method": "single",
					"url":    storage.URL,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/uploads/upload_123/complete":
			var input CompleteUploadInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				http.Error(w, "decode failed", http.StatusBadRequest)
				return
			}
			if input.FileID != "file_123" || input.Key != "project/uploads/file_123-note.txt" {
				t.Errorf("complete input = %#v", input)
				http.Error(w, "wrong complete input", http.StatusBadRequest)
				return
			}
			sawComplete.Store(true)
			writeJSON(t, w, http.StatusOK, map[string]any{
				"file": map[string]any{
					"byteSize":   len("hello byteship"),
					"etag":       "etag_123",
					"filename":   "note.txt",
					"id":         "file_123",
					"path":       "uploads/note.txt",
					"status":     "ready",
					"url":        "https://cdn.byteship.cloud/f/p_x7K9mQ/uploads/note.txt",
					"visibility": "public",
				},
				"upload": map[string]any{
					"id":     "upload_123",
					"status": "completed",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	client := NewClient(
		WithAPIKey("bs_test_key"),
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
	)

	var progress []UploadProgress
	uploaded, err := client.Upload(context.Background(), UploadInput{
		ContentType: "text/plain",
		Filename:    "note.txt",
		OnProgress: func(value UploadProgress) {
			progress = append(progress, value)
		},
		Path:       "uploads/note.txt",
		Reader:     strings.NewReader("hello byteship"),
		Visibility: VisibilityPublic,
	})
	if err != nil {
		t.Fatal(err)
	}

	if uploaded.ID != "file_123" || uploaded.Status != FileStatusReady {
		t.Fatalf("uploaded = %#v", uploaded)
	}
	if !sawCreate.Load() || !sawPut.Load() || !sawComplete.Load() {
		t.Fatalf("expected create, put, and complete to run")
	}
	if len(progress) == 0 || progress[len(progress)-1].Percent != 100 {
		t.Fatalf("progress = %#v, want final 100 percent", progress)
	}
}

func TestResourceMethods(t *testing.T) {
	t.Parallel()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/upload-tokens":
			writeJSON(t, w, http.StatusCreated, map[string]any{
				"uploadToken": map[string]any{
					"expiresAt": "2026-05-06T12:15:00Z",
					"token":     "bsut_test",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/files/uploads/note.txt":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"file": map[string]any{
					"byteSize":    14,
					"contentType": "text/plain",
					"createdAt":   "2026-05-06T12:00:00Z",
					"filename":    "note.txt",
					"id":          "file_123",
					"metadata": map[string]any{
						"folder": "uploads",
					},
					"path":       "uploads/note.txt",
					"status":     "ready",
					"url":        nil,
					"visibility": "private",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/files/uploads/note.txt/signed-url":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"signedUrl": map[string]any{
					"expiresAt": "2026-05-06T12:15:00Z",
					"fileId":    "file_123",
					"path":      "uploads/note.txt",
					"url":       "https://cdn.byteship.cloud/f/p_x7K9mQ/uploads/note.txt?token=test",
				},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/files/uploads/note.txt":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"file": map[string]any{
					"id":     "file_123",
					"path":   "uploads/note.txt",
					"status": "deleted",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	client := NewClient(WithAPIKey("bs_test_key"), WithBaseURL(api.URL), WithHTTPClient(api.Client()))

	token, err := client.CreateUploadToken(context.Background(), CreateUploadTokenInput{Folder: "uploads"})
	if err != nil {
		t.Fatal(err)
	}
	if token.UploadToken.Token != "bsut_test" {
		t.Fatalf("token = %#v", token.UploadToken)
	}

	file, err := client.GetFile(context.Background(), "uploads/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if file.File.Visibility != VisibilityPrivate {
		t.Fatalf("visibility = %q", file.File.Visibility)
	}

	signed, err := client.CreateSignedURL(context.Background(), "uploads/note.txt", CreateSignedURLInput{ExpiresInSeconds: 900})
	if err != nil {
		t.Fatal(err)
	}
	if signed.SignedURL.FileID != "file_123" {
		t.Fatalf("signedURL = %#v", signed.SignedURL)
	}
	if signed.SignedURL.Path != "uploads/note.txt" {
		t.Fatalf("signedURL path = %q", signed.SignedURL.Path)
	}

	deleted, err := client.DeleteFile(context.Background(), "uploads/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.File.Status != FileStatusDeleted {
		t.Fatalf("deleted = %#v", deleted.File)
	}
	if deleted.File.Path != "uploads/note.txt" {
		t.Fatalf("deleted path = %q", deleted.File.Path)
	}
}

func TestAPIErrorIncludesCodeStatusAndDetails(t *testing.T) {
	t.Parallel()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusRequestEntityTooLarge, map[string]any{
			"error":          "upload_too_large",
			"maxUploadBytes": 1024,
		})
	}))
	defer api.Close()

	client := NewClient(WithAPIKey("bs_test_key"), WithBaseURL(api.URL), WithHTTPClient(api.Client()))

	_, err := client.CreateUpload(context.Background(), CreateUploadInput{
		ByteSize:    2048,
		ContentType: "text/plain",
		Filename:    "note.txt",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	byteshipError, ok := AsError(err)
	if !ok {
		t.Fatalf("error = %T, want *Error", err)
	}
	if byteshipError.Code != "upload_too_large" || byteshipError.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("byteshipError = %#v", byteshipError)
	}
	if byteshipError.Details["maxUploadBytes"].(float64) != 1024 {
		t.Fatalf("details = %#v", byteshipError.Details)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}
