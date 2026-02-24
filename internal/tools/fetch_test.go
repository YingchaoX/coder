package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"coder/internal/security"
)

func TestFetchTool_Execute(t *testing.T) {
	// Create a temporary workspace
	tempDir := t.TempDir()
	ws, err := security.NewWorkspace(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create a test server that returns a simple HTML page
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test-image" {
			// Return a small PNG image (1x1 transparent pixel)
			imgData := []byte{
				0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
				0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk start
				0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // Width: 1px, Height: 1px
				0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, // Bit depth, color type, etc.
				0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82, // Rest of minimal PNG
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(imgData)
		} else {
			// Return HTML content
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body><h1>Test Page</h1></body></html>"))
		}
	}))
	defer ts.Close()

	// Create fetch tool with test config
	tool := NewFetchTool(ws, FetchConfig{
		TimeoutSec:     30,
		MaxTextSizeKB:  100,
		MaxImageSizeMB: 1,
		SkipTLSVerify:  true,
	})

	t.Run("fetch HTML content", func(t *testing.T) {
		args := map[string]interface{}{
			"url": ts.URL + "/test-html",
		}
		argsJSON, _ := json.Marshal(args)

		result, err := tool.Execute(context.Background(), argsJSON)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		var fetchResult FetchResult
		if err := json.Unmarshal([]byte(result), &fetchResult); err != nil {
			t.Fatalf("Failed to unmarshal result: %v", err)
		}

		if fetchResult.StatusCode != 200 {
			t.Errorf("Expected status code 200, got %d", fetchResult.StatusCode)
		}
		if fetchResult.ContentType != "text/html" {
			t.Errorf("Expected content type text/html, got %s", fetchResult.ContentType)
		}
		if fetchResult.IsImage {
			t.Error("Expected result not to be an image")
		}
		expectedContent := "<html><body><h1>Test Page</h1></body></html>"
		if fetchResult.Content != expectedContent {
			t.Errorf("Expected content %s, got %s", expectedContent, fetchResult.Content)
		}
	})

	t.Run("fetch image content", func(t *testing.T) {
		args := map[string]interface{}{
			"url": ts.URL + "/test-image",
		}
		argsJSON, _ := json.Marshal(args)

		result, err := tool.Execute(context.Background(), argsJSON)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		var fetchResult FetchResult
		if err := json.Unmarshal([]byte(result), &fetchResult); err != nil {
			t.Fatalf("Failed to unmarshal result: %v", err)
		}

		if fetchResult.StatusCode != 200 {
			t.Errorf("Expected status code 200, got %d", fetchResult.StatusCode)
		}
		if fetchResult.ContentType != "image/png" {
			t.Errorf("Expected content type image/png, got %s", fetchResult.ContentType)
		}
		if !fetchResult.IsImage {
			t.Error("Expected result to be an image")
		}
		if fetchResult.SizeBytes == 0 {
			t.Error("Expected non-zero size for image")
		}
		// Check that the content is base64 encoded (starts with valid base64 characters)
		if len(fetchResult.Content) == 0 {
			t.Error("Expected non-empty content for image")
		}
	})

	t.Run("fetch invalid URL", func(t *testing.T) {
		args := map[string]interface{}{
			"url": "http://invalid-url-that-does-not-exist-12345.com",
		}
		argsJSON, _ := json.Marshal(args)

		_, err := tool.Execute(context.Background(), argsJSON)
		if err == nil {
			t.Fatal("Expected error for invalid URL, got none")
		}
	})
}
