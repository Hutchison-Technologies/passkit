package passkit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestInMemoryPassTemplate_ConcurrentAccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("remote file data"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("could not parse test server url. %v", err)
	}

	template := NewInMemoryPassTemplate()

	const (
		workers    = 4
		iterations = 50
	)

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(6)

		go func(n int) {
			defer wg.Done()
			for j := range iterations {
				template.AddFileBytes(fmt.Sprintf("file-%d-%d.png", n, j), []byte("data"))
			}
		}(i)

		go func(n int) {
			defer wg.Done()
			for j := range iterations {
				template.AddFileBytesLocalized(fmt.Sprintf("file-%d-%d.strings", n, j), "en", []byte("data"))
			}
		}(i)

		go func() {
			defer wg.Done()
			for range iterations {
				if err := template.AddFileFromURL(BundleIcon, *u); err != nil {
					t.Errorf("could not add file from url. %v", err)
					return
				}
			}
		}()

		go func() {
			defer wg.Done()
			for range iterations {
				if err := template.AddAllFiles(filepath.Join("test", "StoreCard.raw")); err != nil {
					t.Errorf("could not add all files. %v", err)
					return
				}
			}
		}()

		go func() {
			defer wg.Done()
			for range iterations {
				files, err := template.GetAllFiles()
				if err != nil {
					t.Errorf("could not get all files. %v", err)
					return
				}
				for name := range files {
					if len(name) == 0 {
						t.Error("unexpected empty template entry name")
						return
					}
				}
			}
		}()

		go func(n int) {
			defer wg.Done()
			base := t.TempDir()
			for j := range 10 {
				dir := filepath.Join(base, fmt.Sprintf("pass-%d-%d", n, j))
				if err := template.ProvisionPassAtDirectory(dir); err != nil {
					t.Errorf("could not provision pass at directory. %v", err)
					return
				}
				_ = os.RemoveAll(dir)
			}
		}(i)
	}

	wg.Wait()
}

func TestInMemoryPassTemplate_GetAllFilesReturnsSnapshot(t *testing.T) {
	template := NewInMemoryPassTemplate()
	template.AddFileBytes(BundleIcon, []byte("icon"))

	files, err := template.GetAllFiles()
	if err != nil {
		t.Fatalf("could not get all files. %v", err)
	}

	files["injected.png"] = []byte("injected")
	template.AddFileBytes(BundleLogo, []byte("logo"))

	current, err := template.GetAllFiles()
	if err != nil {
		t.Fatalf("could not get all files. %v", err)
	}

	if _, ok := current["injected.png"]; ok {
		t.Error("mutating the map returned by GetAllFiles should not affect the template")
	}

	if _, ok := files[BundleLogo]; ok {
		t.Error("files added after GetAllFiles should not show up in a previously returned map")
	}
}

func TestInMemoryPassTemplate_AddFileFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			_, _ = w.Write([]byte("remote file data"))
		case "/server-error.png":
			http.Error(w, "server error", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "successful response is added", path: "/ok.png", wantErr: false},
		{name: "not found response is rejected", path: "/missing.png", wantErr: true},
		{name: "server error response is rejected", path: "/server-error.png", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("could not parse test server url. %v", err)
			}

			template := NewInMemoryPassTemplate()
			err = template.AddFileFromURL(BundleIcon, *u)
			if (err != nil) != tt.wantErr {
				t.Fatalf("AddFileFromURL() error = %v, wantErr %v", err, tt.wantErr)
			}

			files, err := template.GetAllFiles()
			if err != nil {
				t.Fatalf("could not get all files. %v", err)
			}

			data, ok := files[BundleIcon]
			if tt.wantErr {
				if ok {
					t.Errorf("file should not be added when download fails, got %q", data)
				}
				return
			}

			if string(data) != "remote file data" {
				t.Errorf("unexpected file data. got %q", data)
			}
		})
	}
}

func TestInMemoryPassTemplate_AddFileFromURLLocalizedRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL + "/missing.png")
	if err != nil {
		t.Fatalf("could not parse test server url. %v", err)
	}

	template := NewInMemoryPassTemplate()
	if err := template.AddFileFromURLLocalized(BundleIcon, "en", *u); err == nil {
		t.Fatal("expected error for not found response")
	}

	files, err := template.GetAllFiles()
	if err != nil {
		t.Fatalf("could not get all files. %v", err)
	}

	if len(files) != 0 {
		t.Errorf("no files should be added when download fails, got %d", len(files))
	}
}
