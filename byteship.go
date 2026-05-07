// Package byteship provides a Go client for the Byteship file upload API.
package byteship

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.byteship.dev"
const defaultMultipartPartSizeBytes int64 = 16 * 1024 * 1024
const defaultMultipartConcurrency = 4

// Client talks to the Byteship API.
type Client struct {
	authToken  string
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey authenticates the client with a project API key.
func WithAPIKey(apiKey string) Option {
	return func(client *Client) {
		client.authToken = apiKey
	}
}

// WithUploadToken authenticates the client with a short-lived upload token.
func WithUploadToken(uploadToken string) Option {
	return func(client *Client) {
		client.authToken = uploadToken
	}
}

// WithBaseURL overrides the Byteship API origin.
func WithBaseURL(baseURL string) Option {
	return func(client *Client) {
		client.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithHTTPClient overrides the HTTP client used for API and upload requests.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) {
		if httpClient != nil {
			client.httpClient = httpClient
		}
	}
}

// NewClient creates a Byteship API client.
func NewClient(options ...Option) *Client {
	client := &Client{
		baseURL:    defaultBaseURL,
		httpClient: http.DefaultClient,
	}

	for _, option := range options {
		option(client)
	}

	return client
}

// Visibility controls whether a file is publicly deliverable or private.
type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityPublic  Visibility = "public"
)

// FileStatus is the lifecycle state of a Byteship file.
type FileStatus string

const (
	FileStatusPending   FileStatus = "pending"
	FileStatusUploading FileStatus = "uploading"
	FileStatusReady     FileStatus = "ready"
	FileStatusFailed    FileStatus = "failed"
	FileStatusDeleted   FileStatus = "deleted"
)

// UploadMethod is the requested or returned storage upload mode.
type UploadMethod string

const (
	UploadMethodAuto      UploadMethod = "auto"
	UploadMethodSingle    UploadMethod = "single"
	UploadMethodMultipart UploadMethod = "multipart"
)

// UploadSessionStatus is the lifecycle state of an upload session.
type UploadSessionStatus string

const (
	UploadSessionStatusPending   UploadSessionStatus = "pending"
	UploadSessionStatusCompleted UploadSessionStatus = "completed"
	UploadSessionStatusAborted   UploadSessionStatus = "aborted"
	UploadSessionStatusExpired   UploadSessionStatus = "expired"
)

// CreateUploadTokenInput scopes a browser-safe upload token.
type CreateUploadTokenInput struct {
	ExpiresInSeconds int        `json:"expiresInSeconds,omitempty"`
	Folder           string     `json:"folder,omitempty"`
	MaxUploadBytes   int64      `json:"maxUploadBytes,omitempty"`
	Visibility       Visibility `json:"visibility,omitempty"`
}

// CreateUploadTokenResponse is returned after creating an upload token.
type CreateUploadTokenResponse struct {
	UploadToken UploadToken `json:"uploadToken"`
}

// UploadToken is a short-lived token that can create and complete uploads.
type UploadToken struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Token     string    `json:"token"`
}

// CreateUploadInput creates an upload session and presigned storage URL.
type CreateUploadInput struct {
	ByteSize       int64          `json:"byteSize"`
	ChecksumSHA256 string         `json:"checksumSha256,omitempty"`
	ContentType    string         `json:"contentType"`
	Filename       string         `json:"filename"`
	Folder         string         `json:"folder,omitempty"`
	Method         UploadMethod   `json:"method,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	PartSizeBytes  int64          `json:"partSizeBytes,omitempty"`
	Visibility     Visibility     `json:"visibility,omitempty"`
}

// CreateFileUploadInput creates or replaces an upload session for a path.
type CreateFileUploadInput struct {
	ByteSize       int64          `json:"byteSize"`
	ChecksumSHA256 string         `json:"checksumSha256,omitempty"`
	ContentType    string         `json:"contentType"`
	Method         UploadMethod   `json:"method,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	PartSizeBytes  int64          `json:"partSizeBytes,omitempty"`
	Visibility     Visibility     `json:"visibility,omitempty"`
}

// CreateUploadResponse is returned after creating an upload session.
type CreateUploadResponse struct {
	File   PendingFile   `json:"file"`
	Upload UploadSession `json:"upload"`
}

// PendingFile is the file record returned before storage upload completion.
type PendingFile struct {
	ID     string     `json:"id"`
	Path   string     `json:"path"`
	Status FileStatus `json:"status"`
	URL    *string    `json:"url"`
}

// UploadSession contains the presigned URL and completion keys for an upload.
type UploadSession struct {
	ExpiresAt     time.Time         `json:"expiresAt"`
	FileID        string            `json:"fileId"`
	Headers       map[string]string `json:"headers"`
	ID            string            `json:"id"`
	Key           string            `json:"key"`
	Method        UploadMethod      `json:"method"`
	PartCount     int               `json:"partCount,omitempty"`
	PartSizeBytes int64             `json:"partSizeBytes,omitempty"`
	UploadedParts []UploadPart      `json:"uploadedParts,omitempty"`
	URL           string            `json:"url,omitempty"`
}

// UploadPart is a completed multipart storage part.
type UploadPart struct {
	ByteSize   int64  `json:"byteSize"`
	ETag       string `json:"etag"`
	PartNumber int    `json:"partNumber"`
}

// UploadPartURL is a presigned storage URL for one multipart upload part.
type UploadPartURL struct {
	Headers    map[string]string `json:"headers"`
	PartNumber int               `json:"partNumber"`
	URL        string            `json:"url"`
}

// CreateUploadPartURLsInput signs part URLs and records completed parts.
type CreateUploadPartURLsInput struct {
	CompletedParts []UploadPart `json:"completedParts,omitempty"`
	PartNumbers    []int        `json:"partNumbers,omitempty"`
	UploadID       string       `json:"uploadId"`
}

// CreateUploadPartURLsResponse is returned by the multipart part endpoint.
type CreateUploadPartURLsResponse struct {
	PartURLs      []UploadPartURL `json:"partUrls"`
	UploadedParts []UploadPart    `json:"uploadedParts"`
}

// CompleteUploadInput completes a storage upload session.
type CompleteUploadInput struct {
	FileID string `json:"fileId"`
	Key    string `json:"key"`
}

// CompletePathUploadInput completes a path-native single or multipart upload.
type CompletePathUploadInput struct {
	CompletedParts []UploadPart `json:"completedParts,omitempty"`
	UploadID       string       `json:"uploadId"`
}

// CompleteUploadResponse is returned after completing an upload.
type CompleteUploadResponse struct {
	File   UploadedFile           `json:"file"`
	Upload CompletedUploadSession `json:"upload"`
}

// UploadedFile is a ready file returned by Upload or CompleteUpload.
type UploadedFile struct {
	ByteSize   int64      `json:"byteSize"`
	ETag       *string    `json:"etag,omitempty"`
	Filename   string     `json:"filename"`
	ID         string     `json:"id"`
	Path       string     `json:"path"`
	Status     FileStatus `json:"status"`
	URL        *string    `json:"url"`
	Visibility Visibility `json:"visibility"`
}

// CompletedUploadSession is the completed upload state.
type CompletedUploadSession struct {
	ID     string              `json:"id"`
	Status UploadSessionStatus `json:"status"`
}

// CreateSignedURLInput configures a temporary delivery URL.
type CreateSignedURLInput struct {
	ExpiresInSeconds int `json:"expiresInSeconds,omitempty"`
}

// CreateSignedURLResponse is returned after creating a signed URL.
type CreateSignedURLResponse struct {
	SignedURL SignedURL `json:"signedUrl"`
}

// SignedURL is a temporary URL for reading a private file.
type SignedURL struct {
	ExpiresAt time.Time `json:"expiresAt"`
	FileID    string    `json:"fileId"`
	Path      string    `json:"path,omitempty"`
	URL       string    `json:"url"`
}

// GetFileResponse is returned for file metadata lookups.
type GetFileResponse struct {
	File File `json:"file"`
}

// File is a Byteship file metadata record.
type File struct {
	ByteSize    int64          `json:"byteSize"`
	ContentType string         `json:"contentType"`
	CreatedAt   time.Time      `json:"createdAt"`
	Filename    string         `json:"filename"`
	ID          string         `json:"id"`
	Metadata    map[string]any `json:"metadata"`
	Path        string         `json:"path"`
	Status      FileStatus     `json:"status"`
	URL         *string        `json:"url"`
	Visibility  Visibility     `json:"visibility"`
}

// DeleteFileResponse is returned after deleting a file.
type DeleteFileResponse struct {
	File DeletedFile `json:"file"`
}

// DeletedFile is the tombstone state returned for a deleted file.
type DeletedFile struct {
	ID     string     `json:"id"`
	Path   string     `json:"path,omitempty"`
	Status FileStatus `json:"status"`
}

// UploadInput describes a full create-upload, PUT, complete-upload flow.
type UploadInput struct {
	ByteSize             int64
	ChecksumSHA256       string
	ContentType          string
	Filename             string
	Folder               string
	Method               UploadMethod
	Metadata             map[string]any
	MultipartConcurrency int
	OnProgress           func(UploadProgress)
	PartSizeBytes        int64
	Path                 string
	Reader               io.Reader
	Visibility           Visibility
}

// UploadProgress reports bytes written to the presigned storage URL.
type UploadProgress struct {
	Loaded  int64
	Percent float64
	Total   int64
}

// UploadManyInput describes a batch upload.
type UploadManyInput struct {
	Concurrency    int
	Files          []UploadInput
	OnFileProgress func(UploadManyProgress)
}

// UploadManyProgress reports progress for one file in a batch upload.
type UploadManyProgress struct {
	File  UploadInput
	Index int
	UploadProgress
}

// UploadManyResult is one settled batch upload result.
type UploadManyResult struct {
	Error  error
	File   *UploadedFile
	Input  UploadInput
	Status string
}

// Error is a structured Byteship API or upload error.
type Error struct {
	Code       string
	Details    map[string]any
	Err        error
	Message    string
	StatusCode int
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CreateUploadToken creates a short-lived upload token for browser clients.
func (c *Client) CreateUploadToken(ctx context.Context, input CreateUploadTokenInput) (*CreateUploadTokenResponse, error) {
	var output CreateUploadTokenResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/upload-tokens", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// CreateUpload creates an upload session and presigned storage URL.
func (c *Client) CreateUpload(ctx context.Context, input CreateUploadInput) (*CreateUploadResponse, error) {
	var output CreateUploadResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/uploads", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// CreateFileUpload creates or replaces an upload session for a file path.
func (c *Client) CreateFileUpload(ctx context.Context, path string, input CreateFileUploadInput) (*CreateUploadResponse, error) {
	var output CreateUploadResponse
	endpointPath := "/v1/files/" + quoteFilePath(path)
	if err := c.doJSON(ctx, http.MethodPut, endpointPath, input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// CompleteUpload completes an upload session after bytes have reached storage.
func (c *Client) CompleteUpload(ctx context.Context, uploadID string, input CompleteUploadInput) (*CompleteUploadResponse, error) {
	var output CompleteUploadResponse
	path := "/v1/uploads/" + url.PathEscape(uploadID) + "/complete"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// CreateUploadPartURLs signs multipart part URLs and records completed parts.
func (c *Client) CreateUploadPartURLs(ctx context.Context, filePath string, input CreateUploadPartURLsInput) (*CreateUploadPartURLsResponse, error) {
	var output CreateUploadPartURLsResponse
	path := "/v1/files/" + quoteFilePath(filePath) + "/upload/parts"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// CompletePathUpload completes a path-native single or multipart upload.
func (c *Client) CompletePathUpload(ctx context.Context, filePath string, input CompletePathUploadInput) (*CompleteUploadResponse, error) {
	var output CompleteUploadResponse
	path := "/v1/files/" + quoteFilePath(filePath) + "/upload/complete"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// CreateSignedURL creates a temporary URL for a ready private file.
func (c *Client) CreateSignedURL(ctx context.Context, filePathOrID string, input CreateSignedURLInput) (*CreateSignedURLResponse, error) {
	var output CreateSignedURLResponse
	path := "/v1/files/" + quoteFilePath(filePathOrID) + "/signed-url"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// GetFile fetches file metadata.
func (c *Client) GetFile(ctx context.Context, filePathOrID string) (*GetFileResponse, error) {
	var output GetFileResponse
	path := "/v1/files/" + quoteFilePath(filePathOrID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// DeleteFile deletes a file and returns its deleted state.
func (c *Client) DeleteFile(ctx context.Context, filePathOrID string) (*DeleteFileResponse, error) {
	var output DeleteFileResponse
	path := "/v1/files/" + quoteFilePath(filePathOrID)
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// Upload creates an upload session, streams bytes to storage, and completes it.
func (c *Client) Upload(ctx context.Context, input UploadInput) (*UploadedFile, error) {
	if input.Reader == nil {
		return nil, &Error{Code: "missing_reader", Message: "byteship: upload reader is required"}
	}

	byteSize, err := uploadByteSize(input)
	if err != nil {
		return nil, err
	}

	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	filename := strings.TrimSpace(input.Filename)
	if filename == "" {
		filename = "upload"
	}

	method := input.Method
	if method == "" {
		method = UploadMethodAuto
	}

	var created *CreateUploadResponse
	if strings.TrimSpace(input.Path) == "" {
		created, err = c.CreateUpload(ctx, CreateUploadInput{
			ByteSize:       byteSize,
			ChecksumSHA256: input.ChecksumSHA256,
			ContentType:    contentType,
			Filename:       filename,
			Folder:         input.Folder,
			Method:         method,
			Metadata:       input.Metadata,
			PartSizeBytes:  input.PartSizeBytes,
			Visibility:     input.Visibility,
		})
	} else {
		created, err = c.CreateFileUpload(ctx, input.Path, CreateFileUploadInput{
			ByteSize:       byteSize,
			ChecksumSHA256: input.ChecksumSHA256,
			ContentType:    contentType,
			Method:         method,
			Metadata:       input.Metadata,
			PartSizeBytes:  input.PartSizeBytes,
			Visibility:     input.Visibility,
		})
	}
	if err != nil {
		return nil, err
	}

	if created.Upload.Method == UploadMethodMultipart {
		return c.uploadMultipart(ctx, input, created, byteSize)
	}

	if strings.TrimSpace(created.Upload.URL) == "" {
		return nil, &Error{Code: "missing_upload_url", Message: "Byteship did not return an upload URL."}
	}

	if _, err := c.uploadToURL(ctx, created.Upload.URL, input.Reader, byteSize, created.Upload.Headers, input.OnProgress); err != nil {
		return nil, err
	}

	completed, err := c.CompleteUpload(ctx, created.Upload.ID, CompleteUploadInput{
		FileID: created.File.ID,
		Key:    created.Upload.Key,
	})
	if err != nil {
		return nil, err
	}

	return &completed.File, nil
}

// UploadMany uploads a batch of files with bounded concurrency.
func (c *Client) UploadMany(ctx context.Context, input UploadManyInput) []UploadManyResult {
	results := make([]UploadManyResult, len(input.Files))
	if len(input.Files) == 0 {
		return results
	}

	concurrency := input.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	if concurrency > len(input.Files) {
		concurrency = len(input.Files)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				uploadInput := input.Files[index]
				uploadInput.OnProgress = composeProgress(uploadInput.OnProgress, func(progress UploadProgress) {
					if input.OnFileProgress != nil {
						input.OnFileProgress(UploadManyProgress{
							File:           input.Files[index],
							Index:          index,
							UploadProgress: progress,
						})
					}
				})

				file, err := c.Upload(ctx, uploadInput)
				results[index] = UploadManyResult{
					Error:  err,
					File:   file,
					Input:  input.Files[index],
					Status: uploadManyStatus(err),
				}
			}
		}()
	}

	for index := range input.Files {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	return results
}

func (c *Client) doJSON(ctx context.Context, method string, path string, input any, output any) error {
	if strings.TrimSpace(c.authToken) == "" {
		return &Error{Code: "missing_auth_token", Message: "byteship: api key or upload token is required"}
	}

	endpoint, err := c.endpoint(path)
	if err != nil {
		return err
	}

	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("byteship: encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "Bearer "+c.authToken)
	request.Header.Set("accept", "application/json")
	if input != nil {
		request.Header.Set("content-type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("byteship: api request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errorFromResponse(response)
	}

	if output == nil {
		return nil
	}

	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("byteship: decode response: %w", err)
	}

	return nil
}

func (c *Client) endpoint(path string) (string, error) {
	base := strings.TrimRight(c.baseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	endpoint, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}
	return endpoint.String(), nil
}

func quoteFilePath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	encoded := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		encoded = append(encoded, url.PathEscape(segment))
	}
	return strings.Join(encoded, "/")
}

func (c *Client) uploadToURL(ctx context.Context, uploadURL string, reader io.Reader, byteSize int64, headers map[string]string, onProgress func(UploadProgress)) (http.Header, error) {
	body := reader
	if onProgress != nil {
		body = &progressReader{
			onProgress: onProgress,
			reader:     reader,
			total:      byteSize,
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, body)
	if err != nil {
		return nil, err
	}
	request.ContentLength = byteSize

	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("byteship: upload failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		details := readResponseDetails(response.Body)
		return nil, &Error{
			Code:       "upload_failed",
			Details:    details,
			Message:    "Upload failed with status " + strconv.Itoa(response.StatusCode),
			StatusCode: response.StatusCode,
		}
	}

	if onProgress != nil {
		onProgress(UploadProgress{
			Loaded:  byteSize,
			Percent: 100,
			Total:   byteSize,
		})
	}

	return response.Header.Clone(), nil
}

func (c *Client) uploadMultipart(ctx context.Context, input UploadInput, created *CreateUploadResponse, byteSize int64) (*UploadedFile, error) {
	readerAt, ok := input.Reader.(io.ReaderAt)
	if !ok {
		return nil, &Error{
			Code:    "multipart_reader_not_seekable",
			Message: "Multipart uploads require a reader that implements io.ReaderAt.",
		}
	}

	baseOffset := int64(0)
	if seeker, ok := input.Reader.(io.Seeker); ok {
		current, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, fmt.Errorf("byteship: inspect multipart reader: %w", err)
		}
		baseOffset = current
	}

	partSizeBytes := created.Upload.PartSizeBytes
	if partSizeBytes <= 0 {
		partSizeBytes = defaultMultipartPartSizeBytes
	}

	partCount := created.Upload.PartCount
	if partCount <= 0 {
		partCount = int(math.Max(1, math.Ceil(float64(byteSize)/float64(partSizeBytes))))
	}

	concurrency := input.MultipartConcurrency
	if concurrency <= 0 {
		concurrency = defaultMultipartConcurrency
	}
	if concurrency > partCount {
		concurrency = partCount
	}
	if concurrency < 1 {
		concurrency = 1
	}

	uploadID := created.Upload.ID
	path := created.File.Path
	loadedByPartNumber := make(map[int]int64)
	completedParts := make(map[int]UploadPart)
	var progressMu sync.Mutex

	markUploaded := func(part UploadPart) {
		progressMu.Lock()
		completedParts[part.PartNumber] = part
		loadedByPartNumber[part.PartNumber] = part.ByteSize
		progressMu.Unlock()
	}

	for _, part := range created.Upload.UploadedParts {
		markUploaded(part)
	}

	reportProgress := func() {
		if input.OnProgress == nil {
			return
		}
		progressMu.Lock()
		loaded := int64(0)
		for _, value := range loadedByPartNumber {
			loaded += value
		}
		progressMu.Unlock()
		input.OnProgress(UploadProgress{
			Loaded:  loaded,
			Percent: progressPercent(loaded, byteSize),
			Total:   byteSize,
		})
	}

	reportProgress()

	uploadPart := func(ctx context.Context, partNumber int) error {
		progressMu.Lock()
		_, alreadyCompleted := completedParts[partNumber]
		progressMu.Unlock()
		if alreadyCompleted {
			return nil
		}

		partURLResponse, err := c.CreateUploadPartURLs(ctx, path, CreateUploadPartURLsInput{
			PartNumbers: []int{partNumber},
			UploadID:    uploadID,
		})
		if err != nil {
			return err
		}

		for _, part := range partURLResponse.UploadedParts {
			markUploaded(part)
		}

		progressMu.Lock()
		_, alreadyCompleted = completedParts[partNumber]
		progressMu.Unlock()
		if alreadyCompleted {
			reportProgress()
			return nil
		}

		var partURL *UploadPartURL
		for index := range partURLResponse.PartURLs {
			if partURLResponse.PartURLs[index].PartNumber == partNumber {
				partURL = &partURLResponse.PartURLs[index]
				break
			}
		}
		if partURL == nil {
			return &Error{
				Code:    "missing_part_upload_url",
				Message: "Byteship did not return an upload URL for part " + strconv.Itoa(partNumber) + ".",
			}
		}

		start := int64(partNumber-1) * partSizeBytes
		partByteSize := multipartPartByteSize(byteSize, partSizeBytes, partCount, partNumber)
		partReader := io.NewSectionReader(readerAt, baseOffset+start, partByteSize)
		partProgress := func(progress UploadProgress) {
			progressMu.Lock()
			loadedByPartNumber[partNumber] = progress.Loaded
			progressMu.Unlock()
			reportProgress()
		}

		headers, err := c.uploadToURL(ctx, partURL.URL, partReader, partByteSize, partURL.Headers, partProgress)
		if err != nil {
			return err
		}

		etag := headers.Get("etag")
		if etag == "" {
			return &Error{
				Code:    "missing_part_etag",
				Message: "Storage did not return an ETag for part " + strconv.Itoa(partNumber) + ".",
			}
		}

		completedPart := UploadPart{
			ByteSize:   partByteSize,
			ETag:       etag,
			PartNumber: partNumber,
		}
		if _, err := c.CreateUploadPartURLs(ctx, path, CreateUploadPartURLsInput{
			CompletedParts: []UploadPart{completedPart},
			UploadID:       uploadID,
		}); err != nil {
			return err
		}

		markUploaded(completedPart)
		reportProgress()
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for partNumber := range jobs {
				if err := uploadPart(ctx, partNumber); err != nil {
					select {
					case errCh <- err:
						cancel()
					default:
					}
				}
			}
		}()
	}

sendLoop:
	for partNumber := 1; partNumber <= partCount; partNumber++ {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- partNumber:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return nil, err
	}

	completed, err := c.CompletePathUpload(ctx, path, CompletePathUploadInput{
		UploadID: uploadID,
	})
	if err != nil {
		return nil, err
	}

	return &completed.File, nil
}

func errorFromResponse(response *http.Response) error {
	details := readResponseDetails(response.Body)
	code := "api_request_failed"
	message := "Byteship API request failed with status " + strconv.Itoa(response.StatusCode)

	if value, ok := details["error"].(string); ok && value != "" {
		code = value
	}
	if value, ok := details["detail"].(string); ok && value != "" {
		message = value
	}

	return &Error{
		Code:       code,
		Details:    details,
		Message:    message,
		StatusCode: response.StatusCode,
	}
}

func readResponseDetails(reader io.Reader) map[string]any {
	var details map[string]any
	if err := json.NewDecoder(reader).Decode(&details); err != nil {
		return nil
	}
	return details
}

func uploadByteSize(input UploadInput) (int64, error) {
	if input.ByteSize > 0 {
		return input.ByteSize, nil
	}

	switch reader := input.Reader.(type) {
	case interface{ Len() int }:
		return int64(reader.Len()), nil
	case interface{ Size() int64 }:
		return reader.Size(), nil
	case io.Seeker:
		current, err := reader.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, fmt.Errorf("byteship: inspect upload reader: %w", err)
		}
		end, err := reader.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, fmt.Errorf("byteship: inspect upload reader: %w", err)
		}
		if _, err := reader.Seek(current, io.SeekStart); err != nil {
			return 0, fmt.Errorf("byteship: restore upload reader: %w", err)
		}
		if end < current {
			return 0, &Error{Code: "invalid_byte_size", Message: "byteship: upload reader position is past the end"}
		}
		return end - current, nil
	default:
		return 0, &Error{Code: "missing_byte_size", Message: "byteship: upload byte size is required for non-seekable readers"}
	}
}

type progressReader struct {
	loaded     int64
	onProgress func(UploadProgress)
	reader     io.Reader
	total      int64
}

func (reader *progressReader) Read(p []byte) (int, error) {
	n, err := reader.reader.Read(p)
	if n > 0 {
		reader.loaded += int64(n)
		reader.onProgress(UploadProgress{
			Loaded:  reader.loaded,
			Percent: progressPercent(reader.loaded, reader.total),
			Total:   reader.total,
		})
	}
	return n, err
}

func progressPercent(loaded int64, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return math.Min(100, (float64(loaded)/float64(total))*100)
}

func multipartPartByteSize(byteSize int64, partSizeBytes int64, partCount int, partNumber int) int64 {
	if partNumber < partCount {
		return partSizeBytes
	}
	remaining := byteSize - (partSizeBytes * int64(partCount-1))
	if remaining < 0 {
		return 0
	}
	return remaining
}

func composeProgress(first func(UploadProgress), second func(UploadProgress)) func(UploadProgress) {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return func(progress UploadProgress) {
		first(progress)
		second(progress)
	}
}

func uploadManyStatus(err error) string {
	if err != nil {
		return "rejected"
	}
	return "fulfilled"
}

// AsError reports whether err contains a Byteship Error.
func AsError(err error) (*Error, bool) {
	var byteshipError *Error
	if errors.As(err, &byteshipError) {
		return byteshipError, true
	}
	return nil, false
}
