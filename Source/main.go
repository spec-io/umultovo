package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type catalogSearch struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Mode         string   `json:"mode"`
	Center       string   `json:"center,omitempty"`
	Radius       float64  `json:"radius,omitempty"`
	Parcel       string   `json:"parcel,omitempty"`
	Streets      []string `json:"streets,omitempty"`
	Files        []string `json:"files"`
	Query        string   `json:"query,omitempty"`
	Objects      string   `json:"objects,omitempty"`
	Transactions string   `json:"transactions,omitempty"`
	Default      bool     `json:"default,omitempty"`
	CreatedAt    string   `json:"createdAt,omitempty"`
}

type createSearchRequest struct {
	Name    string   `json:"name"`
	Mode    string   `json:"mode"`
	Files   []string `json:"files"`
	Parcel  string   `json:"parcel"`
	Center  string   `json:"center"`
	Radius  float64  `json:"radius"`
	Streets []string `json:"streets"`
	Query   string   `json:"query"`
}

var catalogMu sync.Mutex
var rcnArchiveMu sync.Mutex

const maxUploadBytes = 8 << 30
const maxRCNArchiveBytes = 1 << 30
const rcnArchivePageURL = "https://bip.geopoz.poznan.pl/gpb/miejski-osrodek-dokumen/dane-do-pobrania-rejestr-cen-n/2234,Dane-do-pobrania-Rejestr-Cen-Nieruchomosci.html"
const rcnHTTPUserAgent = "Umultovo-RCN/1.0 (local importer)"

type rcnArchive struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Info            string   `json:"info"`
	Files           []string `json:"files,omitempty"`
	Downloaded      bool     `json:"downloaded"`
	Managed         bool     `json:"managed"`
	UpdateAvailable bool     `json:"updateAvailable"`
	FreshnessKnown  bool     `json:"freshnessKnown"`
	ETag            string   `json:"-"`
	URL             string   `json:"-"`
}

type rcnArchiveList struct {
	Archives   []rcnArchive   `json:"archives"`
	LocalFiles []localRCNFile `json:"localFiles"`
}

type localRCNFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type downloadedRCNArchive struct {
	Files        []string `json:"files"`
	Info         string   `json:"info,omitempty"`
	ETag         string   `json:"etag,omitempty"`
	LastModified string   `json:"lastModified,omitempty"`
	SizeBytes    int64    `json:"sizeBytes,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
}

var rcnArchiveLinkPattern = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a\s*>`)
var rcnArchiveTitlePattern = regexp.MustCompile(`(?is)<strong\b[^>]*>(.*?)</strong\s*>`)
var rcnArchiveInfoPattern = regexp.MustCompile(`(?is)<span\b[^>]*>(.*?)</span\s*>`)
var htmlTagPattern = regexp.MustCompile(`(?is)<[^>]*>`)

type point struct {
	X float64
	Y float64
}

type reference struct {
	Kind string
	ID   string
}

type feature struct {
	Type   string
	ID     string
	Source string
	Fields map[string]string
	Refs   []reference
	Points []point
}

type xmlNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Text    string     `xml:",chardata"`
	Nodes   []xmlNode  `xml:",any"`
}

type radiusFeatureRow struct {
	Type       string
	ID         string
	ParcelID   string
	Street     string
	Number     string
	Area       string
	Price      string
	PricePerM2 string
	Geometry   string
	Distance   string
	Source     string
}

type radiusTransactionRow struct {
	Date       string
	Number     string
	Kind       string
	Market     string
	Price      string
	Area       string
	ShareArea  string
	Shares     string
	PricePerM2 string
	Addresses  string
	ParcelIDs  string
	Matched    string
	Distance   string
	Source     string
	ID         string
}

type nearbyFeature struct {
	Feature  feature
	Distance float64
}

var timestampSuffix = regexp.MustCompile(`_\d{4}-\d{2}-\d{2}T[\d\-:]+$`)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "radius":
		err = runRadius(os.Args[2:])
	case "streets":
		err = runStreets(os.Args[2:])
	case "serve":
		err = runServer(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `RCN transaction data processor

Usage:
  go run . radius [options] [GML files...]
  go run . streets [options] [GML files...]
  go run . serve [--addr 127.0.0.1:8080]

Commands:
  radius  Export parcel outlines and transactions near a parcel or WGS84 center.
  streets Export flattened transactions matching street names.
  serve   Run the local read/write website and RCN import API.

With no file arguments, commands read *.gml from ../RCN.`)
}

func projectRoot() (string, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(workingDirectory, "index.html")); err == nil {
		return workingDirectory, nil
	}
	parent := filepath.Dir(workingDirectory)
	if _, err := os.Stat(filepath.Join(parent, "index.html")); err == nil {
		return parent, nil
	}
	return "", errors.New("run the server from the project root or Source directory")
}

func runServer(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	address := flags.String("addr", "127.0.0.1:8080", "local server address (loopback only)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	containerBind := host == "0.0.0.0" && os.Getenv("RCN_CONTAINER") == "1"
	if ip == nil || (!ip.IsLoopback() && !containerBind) {
		return fmt.Errorf("server must bind to a loopback address, got %q", host)
	}
	root, err := projectRoot()
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/searches", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			serveSearches(root, w)
		case http.MethodPost:
			if !sameOrigin(r) {
				http.Error(w, "cross-origin requests are not allowed", http.StatusForbidden)
				return
			}
			createSearch(root, w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/rcn-files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		listRCNFiles(root, w)
	})
	mux.HandleFunc("/api/rcn-archives", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		listRemoteRCNArchives(r.Context(), root, w)
	})
	mux.HandleFunc("/api/rcn-archives/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin requests are not allowed", http.StatusForbidden)
			return
		}
		downloadRemoteRCNArchive(r.Context(), root, w, r)
	})
	mux.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin requests are not allowed", http.StatusForbidden)
			return
		}
		uploadRCNFiles(root, w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveProjectFile(root, w, r)
	})
	fmt.Fprintf(os.Stderr, "Local RCN workspace: http://%s\n", *address)
	return http.ListenAndServe(*address, mux)
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == "http://"+r.Host
}

func serveJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, "write JSON response:", err)
	}
}

func serveSearches(root string, w http.ResponseWriter) {
	data, err := os.ReadFile(filepath.Join(root, "Data", "searches.json"))
	if err != nil {
		http.Error(w, "cannot read Data/searches.json: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}

func listRCNFiles(root string, w http.ResponseWriter) {
	entries, err := os.ReadDir(filepath.Join(root, "RCN"))
	if err != nil {
		http.Error(w, "cannot list RCN/: "+err.Error(), http.StatusInternalServerError)
		return
	}
	files := make([]localRCNFile, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gml") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			http.Error(w, "cannot inspect RCN file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		files = append(files, localRCNFile{Name: entry.Name(), Size: info.Size()})
	}
	serveJSON(w, files)
}

func listRemoteRCNArchives(ctx context.Context, root string, w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	archives, err := fetchRCNArchives(ctx)
	if err != nil {
		http.Error(w, "cannot read GEOPOZ RCN archive list: "+err.Error(), http.StatusBadGateway)
		return
	}
	manifest, err := readRCNArchiveManifest(root)
	if err != nil {
		http.Error(w, "cannot read local RCN archive state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	localFiles, err := readLocalRCNFiles(root)
	if err != nil {
		http.Error(w, "cannot list RCN/: "+err.Error(), http.StatusInternalServerError)
		return
	}
	localNames := make(map[string]bool, len(localFiles))
	for _, file := range localFiles {
		localNames[file.Name] = true
	}
	for i := range archives {
		state := manifest[archives[i].ID]
		archives[i].Files = append([]string(nil), state.Files...)
		archives[i].Downloaded = len(state.Files) > 0 && allFilesExist(state.Files, localNames)
		archives[i].Managed = archives[i].Downloaded
		if !archives[i].Downloaded {
			for _, name := range expectedRCNGMLNames(archives[i].Name) {
				if localNames[name] {
					archives[i].Files = []string{name}
					archives[i].Downloaded = true
					break
				}
			}
		}
		archives[i].FreshnessKnown = state.Info != "" && archives[i].Info != ""
		archives[i].UpdateAvailable = archives[i].Downloaded &&
			archiveMetadataChanged(archives[i], state)
	}
	serveJSON(w, rcnArchiveList{Archives: archives, LocalFiles: localFiles})
}

func readLocalRCNFiles(root string) ([]localRCNFile, error) {
	entries, err := os.ReadDir(filepath.Join(root, "RCN"))
	if errors.Is(err, os.ErrNotExist) {
		return []localRCNFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]localRCNFile, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gml") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", entry.Name(), err)
		}
		files = append(files, localRCNFile{Name: entry.Name(), Size: info.Size()})
	}
	return files, nil
}

func fetchRCNArchives(ctx context.Context) ([]rcnArchive, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rcnArchivePageURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", rcnHTTPUserAgent)
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GEOPOZ page returned HTTP %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil {
		return nil, fmt.Errorf("read GEOPOZ page: %w", err)
	}
	if len(body) > 4<<20 {
		return nil, errors.New("GEOPOZ page exceeds 4 MiB")
	}
	archives, err := parseRCNArchives(string(body))
	if err != nil {
		return nil, err
	}
	return archives, nil
}

func parseRCNArchives(pageHTML string) ([]rcnArchive, error) {
	matches := rcnArchiveLinkPattern.FindAllStringSubmatch(pageHTML, -1)
	archives := make([]rcnArchive, 0, len(matches))
	base, _ := url.Parse(rcnArchivePageURL)
	for _, match := range matches {
		link, err := url.Parse(html.UnescapeString(strings.TrimSpace(match[1])))
		if err != nil {
			continue
		}
		downloadURL := base.ResolveReference(link)
		if downloadURL.Scheme != "https" || downloadURL.Host != "bip.geopoz.poznan.pl" ||
			!strings.HasPrefix(downloadURL.Path, "/download/") ||
			!strings.EqualFold(path.Ext(downloadURL.Path), ".zip") {
			continue
		}
		titleMatch := rcnArchiveTitlePattern.FindStringSubmatch(match[2])
		if len(titleMatch) != 2 {
			continue
		}
		name := strings.TrimSpace(htmlTagPattern.ReplaceAllString(html.UnescapeString(titleMatch[1]), " "))
		if !strings.HasPrefix(name, "Baza Danych RCN Poznań") {
			continue
		}
		info := ""
		if infoMatch := rcnArchiveInfoPattern.FindStringSubmatch(match[2]); len(infoMatch) == 2 {
			info = strings.Join(strings.Fields(htmlTagPattern.ReplaceAllString(
				html.UnescapeString(infoMatch[1]), " ")), " ")
		}
		sum := sha256.Sum256([]byte(downloadURL.Path))
		archives = append(archives, rcnArchive{
			ID: hex.EncodeToString(sum[:8]), Name: name, Info: info, URL: downloadURL.String(),
		})
	}
	if len(archives) == 0 {
		return nil, fmt.Errorf("GEOPOZ page contains no supported RCN ZIP links among %d HTML links", len(matches))
	}
	return archives, nil
}

func expectedRCNGMLNames(title string) []string {
	title = strings.TrimSpace(title)
	names := []string{title + ".gml"}
	dashName := strings.ReplaceAll(title, " do ", "-")
	if dashName != title {
		names = append(names, dashName+".gml")
	}
	return names
}

func allFilesExist(files []string, localNames map[string]bool) bool {
	if len(files) == 0 {
		return false
	}
	for _, name := range files {
		if !localNames[name] {
			return false
		}
	}
	return true
}

func archiveMetadataChanged(remote rcnArchive, local downloadedRCNArchive) bool {
	return remote.Info != "" && local.Info != "" && remote.Info != local.Info
}

func readRCNArchiveManifest(root string) (map[string]downloadedRCNArchive, error) {
	data, err := os.ReadFile(filepath.Join(root, "Data", "rcn-archives.json"))
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]downloadedRCNArchive), nil
	}
	if err != nil {
		return nil, err
	}
	var manifest map[string]downloadedRCNArchive
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode Data/rcn-archives.json: %w", err)
	}
	if manifest == nil {
		manifest = make(map[string]downloadedRCNArchive)
	}
	return manifest, nil
}

func writeRCNArchiveManifest(root string, manifest map[string]downloadedRCNArchive) error {
	directory := filepath.Join(root, "Data")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create Data/: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".rcn-archives-*.tmp")
	if err != nil {
		return fmt.Errorf("create archive manifest: %w", err)
	}
	defer os.Remove(temporary.Name())
	if err := json.NewEncoder(temporary).Encode(manifest); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write archive manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close archive manifest: %w", err)
	}
	if err := os.Rename(temporary.Name(), filepath.Join(directory, "rcn-archives.json")); err != nil {
		return fmt.Errorf("replace archive manifest: %w", err)
	}
	return nil
}

type stagedRCNFile struct {
	name      string
	temporary string
}

func downloadRemoteRCNArchive(ctx context.Context, root string, w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "rcn-archives" ||
		parts[3] != "download" || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(parts[2]) {
		http.NotFound(w, r)
		return
	}
	rcnArchiveMu.Lock()
	defer rcnArchiveMu.Unlock()

	archives, err := fetchRCNArchives(ctx)
	if err != nil {
		http.Error(w, "cannot verify GEOPOZ archive: "+err.Error(), http.StatusBadGateway)
		return
	}
	var archive *rcnArchive
	for i := range archives {
		if archives[i].ID == parts[2] {
			archive = &archives[i]
			break
		}
	}
	if archive == nil {
		http.Error(w, "RCN archive is no longer listed by GEOPOZ", http.StatusNotFound)
		return
	}
	manifest, err := readRCNArchiveManifest(root)
	if err != nil {
		http.Error(w, "cannot read local RCN archive state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	directory := filepath.Join(root, "RCN")
	if err := os.MkdirAll(directory, 0755); err != nil {
		http.Error(w, "cannot create RCN/: "+err.Error(), http.StatusInternalServerError)
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
	if err != nil {
		http.Error(w, "cannot create GEOPOZ download request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	request.Header.Set("User-Agent", rcnHTTPUserAgent)
	response, err := (&http.Client{Timeout: 3 * time.Minute}).Do(request)
	if err != nil {
		http.Error(w, "download from GEOPOZ failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		http.Error(w, "GEOPOZ archive download returned HTTP "+response.Status, http.StatusBadGateway)
		return
	}
	if response.ContentLength > maxRCNArchiveBytes {
		http.Error(w, "GEOPOZ archive exceeds 1 GiB limit", http.StatusRequestEntityTooLarge)
		return
	}
	archiveFile, err := os.CreateTemp(directory, ".rcn-download-*.zip")
	if err != nil {
		http.Error(w, "create temporary archive: "+err.Error(), http.StatusInternalServerError)
		return
	}
	archivePath := archiveFile.Name()
	defer os.Remove(archivePath)
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(archiveFile, hasher), io.LimitReader(response.Body, maxRCNArchiveBytes+1))
	closeErr := archiveFile.Close()
	if copyErr != nil || closeErr != nil {
		http.Error(w, "save GEOPOZ archive failed", http.StatusInternalServerError)
		return
	}
	if written > maxRCNArchiveBytes {
		http.Error(w, "GEOPOZ archive exceeds 1 GiB limit", http.StatusRequestEntityTooLarge)
		return
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		http.Error(w, "GEOPOZ response is not a valid ZIP archive: "+err.Error(), http.StatusBadGateway)
		return
	}
	staged, err := stageRCNGMLFiles(directory, reader.File)
	closeZipErr := reader.Close()
	if err != nil {
		http.Error(w, "cannot extract GEOPOZ archive: "+err.Error(), http.StatusBadGateway)
		return
	}
	if closeZipErr != nil {
		removeStagedRCNFiles(staged)
		http.Error(w, "cannot close GEOPOZ archive: "+closeZipErr.Error(), http.StatusInternalServerError)
		return
	}
	defer removeStagedRCNFiles(staged)
	if len(staged) == 0 {
		http.Error(w, "GEOPOZ archive contains no GML files", http.StatusBadGateway)
		return
	}

	previous := manifest[archive.ID]
	previousFiles := make(map[string]bool, len(previous.Files))
	for _, name := range previous.Files {
		previousFiles[name] = true
	}
	for _, file := range staged {
		if !validRCNGMLName(file.name) {
			http.Error(w, "archive contains an unsafe GML file name", http.StatusBadGateway)
			return
		}
		if _, err := os.Lstat(filepath.Join(directory, file.name)); err == nil && !previousFiles[file.name] {
			http.Error(w, file.name+" already exists in RCN/ and is not managed by this archive downloader", http.StatusConflict)
			return
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "check local GML file: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	backups := make(map[string]string)
	installed := make([]string, 0, len(staged))
	rollback := func() {
		for _, name := range installed {
			_ = os.Remove(filepath.Join(directory, name))
		}
		for name, backup := range backups {
			_ = os.Rename(backup, filepath.Join(directory, name))
		}
	}
	for _, file := range staged {
		destination := filepath.Join(directory, file.name)
		if previousFiles[file.name] {
			if _, err := os.Stat(destination); err == nil {
				backup, createErr := os.CreateTemp(directory, ".rcn-backup-*.tmp")
				if createErr != nil {
					rollback()
					http.Error(w, "prepare existing GML replacement: "+createErr.Error(), http.StatusInternalServerError)
					return
				}
				backupPath := backup.Name()
				if closeErr := backup.Close(); closeErr != nil {
					_ = os.Remove(backupPath)
					rollback()
					http.Error(w, "prepare existing GML replacement: "+closeErr.Error(), http.StatusInternalServerError)
					return
				}
				if removeErr := os.Remove(backupPath); removeErr != nil {
					rollback()
					http.Error(w, "prepare existing GML replacement: "+removeErr.Error(), http.StatusInternalServerError)
					return
				}
				if renameErr := os.Rename(destination, backupPath); renameErr != nil {
					rollback()
					http.Error(w, "preserve existing GML file: "+renameErr.Error(), http.StatusInternalServerError)
					return
				}
				backups[file.name] = backupPath
			} else if !errors.Is(err, os.ErrNotExist) {
				rollback()
				http.Error(w, "inspect existing GML file: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if err := os.Rename(file.temporary, destination); err != nil {
			rollback()
			http.Error(w, "install "+file.name+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		installed = append(installed, file.name)
	}

	names := make([]string, 0, len(staged))
	for _, file := range staged {
		names = append(names, file.name)
	}
	manifest[archive.ID] = downloadedRCNArchive{
		Files: names, Info: archive.Info, ETag: response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
		SizeBytes:    written, SHA256: hex.EncodeToString(hasher.Sum(nil)),
	}
	if err := writeRCNArchiveManifest(root, manifest); err != nil {
		rollback()
		http.Error(w, "cannot save local RCN archive state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for _, backup := range backups {
		if err := os.Remove(backup); err != nil {
			fmt.Fprintln(os.Stderr, "remove replaced RCN backup:", err)
		}
	}
	serveJSON(w, map[string][]string{"downloaded": names})
}

func stageRCNGMLFiles(directory string, entries []*zip.File) ([]stagedRCNFile, error) {
	staged := make([]stagedRCNFile, 0)
	seen := make(map[string]bool)
	var total int64
	cleanup := func() {
		removeStagedRCNFiles(staged)
	}
	for _, entry := range entries {
		if entry.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(entry.Name), ".gml") {
			continue
		}
		cleanName := path.Clean(strings.ReplaceAll(entry.Name, "\\", "/"))
		if strings.HasPrefix(cleanName, "/") || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
			cleanup()
			return nil, fmt.Errorf("unsafe archive path %q", entry.Name)
		}
		name := path.Base(cleanName)
		if !validRCNGMLName(name) || seen[name] {
			cleanup()
			return nil, fmt.Errorf("invalid or duplicate GML file name %q", entry.Name)
		}
		if entry.Mode()&os.ModeSymlink != 0 || entry.UncompressedSize64 > uint64(maxUploadBytes) {
			cleanup()
			return nil, fmt.Errorf("unsafe or oversized GML entry %q", entry.Name)
		}
		total += int64(entry.UncompressedSize64)
		if total > maxUploadBytes {
			cleanup()
			return nil, errors.New("expanded GML files exceed 8 GiB limit")
		}
		source, err := entry.Open()
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("open %s: %w", entry.Name, err)
		}
		temporary, err := os.CreateTemp(directory, ".rcn-gml-*.tmp")
		if err != nil {
			_ = source.Close()
			cleanup()
			return nil, fmt.Errorf("create staged GML: %w", err)
		}
		staged = append(staged, stagedRCNFile{name: name, temporary: temporary.Name()})
		written, copyErr := io.Copy(temporary, io.LimitReader(source, int64(entry.UncompressedSize64)+1))
		sourceErr := source.Close()
		closeErr := temporary.Close()
		if copyErr != nil || sourceErr != nil || closeErr != nil {
			cleanup()
			return nil, fmt.Errorf("extract %s failed", entry.Name)
		}
		if written != int64(entry.UncompressedSize64) {
			cleanup()
			return nil, fmt.Errorf("unexpected extracted size for %s", entry.Name)
		}
		seen[name] = true
	}
	return staged, nil
}

func removeStagedRCNFiles(files []stagedRCNFile) {
	for _, file := range files {
		_ = os.Remove(file.temporary)
	}
}

func validRCNGMLName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		filepath.Base(name) == name && strings.EqualFold(filepath.Ext(name), ".gml")
}

func uploadRCNFiles(root string, w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart/form-data: "+err.Error(), http.StatusBadRequest)
		return
	}
	directory := filepath.Join(root, "RCN")
	if err := os.MkdirAll(directory, 0755); err != nil {
		http.Error(w, "cannot create RCN/: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var uploaded []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "read upload: "+err.Error(), http.StatusBadRequest)
			return
		}
		if part.FileName() == "" {
			_ = part.Close()
			continue
		}
		name := uploadFileName(part)
		if name == "." || name == "" || !strings.EqualFold(filepath.Ext(name), ".gml") {
			_ = part.Close()
			http.Error(w, "only .gml files can be imported", http.StatusBadRequest)
			return
		}
		destination := filepath.Join(directory, name)
		temporary, err := os.CreateTemp(directory, ".upload-*.tmp")
		if err != nil {
			_ = part.Close()
			http.Error(w, "create upload temporary file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		written, copyErr := io.Copy(temporary, io.LimitReader(part, maxUploadBytes+1))
		partErr := part.Close()
		closeErr := temporary.Close()
		if copyErr != nil || partErr != nil || closeErr != nil {
			_ = os.Remove(temporary.Name())
			http.Error(w, "write upload failed", http.StatusInternalServerError)
			return
		}
		if written > maxUploadBytes {
			_ = os.Remove(temporary.Name())
			http.Error(w, "GML upload exceeds 8 GiB limit", http.StatusRequestEntityTooLarge)
			return
		}
		if _, err := os.Stat(destination); err == nil {
			_ = os.Remove(temporary.Name())
			http.Error(w, name+" already exists in RCN/", http.StatusConflict)
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(temporary.Name())
			http.Error(w, "check destination: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(temporary.Name(), destination); err != nil {
			_ = os.Remove(temporary.Name())
			http.Error(w, "import "+name+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		uploaded = append(uploaded, name)
	}
	serveJSON(w, map[string][]string{"uploaded": uploaded})
}

func createSearch(root string, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var request createSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid search request: "+err.Error(), http.StatusBadRequest)
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		http.Error(w, "search name is required", http.StatusBadRequest)
		return
	}
	if len(request.Files) == 0 {
		http.Error(w, "select at least one RCN file", http.StatusBadRequest)
		return
	}
	if request.Mode == "view" {
		csvPath := filepath.ToSlash(filepath.Clean(request.Files[0]))
		if csvPath != request.Files[0] || !strings.HasPrefix(csvPath, "CSV/") || !strings.EqualFold(filepath.Ext(csvPath), ".csv") {
			http.Error(w, "view searches must reference a CSV/ file", http.StatusBadRequest)
			return
		}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(csvPath))); err != nil || info.IsDir() {
			http.Error(w, "CSV file does not exist: "+csvPath, http.StatusBadRequest)
			return
		}
		catalogMu.Lock()
		defer catalogMu.Unlock()
		entry := catalogSearch{
			ID:   safeSlug(request.Name) + "-" + time.Now().UTC().Format("20060102-150405"),
			Name: request.Name, Description: "Filtr: " + request.Query, Mode: "view",
			Query: request.Query, Files: []string{csvPath}, Transactions: csvPath,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		entries, err := readCatalog(root)
		if err != nil {
			http.Error(w, "cannot read search catalog: "+err.Error(), http.StatusInternalServerError)
			return
		}
		entries = append(entries, entry)
		if err := writeCatalog(root, entries); err != nil {
			http.Error(w, "cannot update search catalog: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(entry)
		return
	}
	inputPaths := make([]string, 0, len(request.Files))
	for _, name := range request.Files {
		if filepath.Base(name) != name || !strings.EqualFold(filepath.Ext(name), ".gml") {
			http.Error(w, "invalid RCN file name", http.StatusBadRequest)
			return
		}
		filePath := filepath.Join(root, "RCN", name)
		if info, err := os.Stat(filePath); err != nil || info.IsDir() {
			http.Error(w, "RCN file does not exist: "+name, http.StatusBadRequest)
			return
		}
		inputPaths = append(inputPaths, filePath)
	}
	if request.Mode == "radius" && (request.Radius <= 0 || math.IsNaN(request.Radius) || math.IsInf(request.Radius, 0)) {
		http.Error(w, "radius must be finite and greater than zero", http.StatusBadRequest)
		return
	}
	var arguments []string
	switch request.Mode {
	case "radius":
		arguments = []string{"radius", "--radius", formatNumber(request.Radius)}
		if request.Center != "" {
			if _, valid, _ := parseCenter(request.Center); !valid {
				http.Error(w, "center must be latitude,longitude in WGS84", http.StatusBadRequest)
				return
			}
			arguments = append(arguments, "--center", request.Center)
		} else if request.Parcel != "" {
			arguments = append(arguments, "--parcel", request.Parcel)
		} else {
			http.Error(w, "provide a reference parcel or WGS84 center", http.StatusBadRequest)
			return
		}
	case "streets":
		request.Streets = cleanStreetNames(request.Streets)
		if len(request.Streets) == 0 {
			http.Error(w, "provide at least one street name", http.StatusBadRequest)
			return
		}
		arguments = []string{"streets", "--streets", strings.Join(request.Streets, ",")}
	default:
		http.Error(w, "mode must be radius or streets", http.StatusBadRequest)
		return
	}

	catalogMu.Lock()
	defer catalogMu.Unlock()
	slug := safeSlug(request.Name) + "-" + time.Now().UTC().Format("20060102-150405")
	prefix := filepath.Join(root, "CSV", slug)
	arguments = append(arguments, "--output", prefix)
	arguments = append(arguments, inputPaths...)
	executable, err := os.Executable()
	if err != nil {
		http.Error(w, "locate processor: "+err.Error(), http.StatusInternalServerError)
		return
	}
	command := exec.Command(executable, arguments...)
	command.Dir = filepath.Join(root, "Source")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		http.Error(w, "RCN processing failed: "+output.String()+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	entry := catalogSearch{
		ID: slug, Name: request.Name, Mode: request.Mode, Radius: request.Radius,
		Center: request.Center, Parcel: request.Parcel, Streets: request.Streets,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if request.Mode == "radius" {
		entry.Objects = "CSV/" + slug + "-obiekty.csv"
		entry.Transactions = "CSV/" + slug + "-transakcje.csv"
		entry.Files = []string{entry.Transactions, entry.Objects}
	} else {
		entry.Transactions = "CSV/" + slug + ".csv"
		entry.Files = []string{entry.Transactions}
	}
	entry.Description = describeSearch(entry)
	entries, err := readCatalog(root)
	if err != nil {
		http.Error(w, "cannot read search catalog: "+err.Error(), http.StatusInternalServerError)
		return
	}
	entries = append(entries, entry)
	if err := writeCatalog(root, entries); err != nil {
		http.Error(w, "cannot update search catalog: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(entry)
}

func cleanStreetNames(names []string) []string {
	cleaned := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" && !contains(cleaned, name) {
			cleaned = append(cleaned, name)
		}
	}
	return cleaned
}

func safeSlug(value string) string {
	var result strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			result.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			result.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(result.String(), "-")
	if slug == "" {
		return "search"
	}
	return slug
}

func describeSearch(entry catalogSearch) string {
	if entry.Mode == "radius" {
		center := entry.Center
		if center == "" {
			center = "działka " + entry.Parcel
		}
		return fmt.Sprintf("%s; promień %.0f m", center, entry.Radius)
	}
	return "Ulice: " + strings.Join(entry.Streets, ", ")
}

func readCatalog(root string) ([]catalogSearch, error) {
	data, err := os.ReadFile(filepath.Join(root, "Data", "searches.json"))
	if err != nil {
		return nil, err
	}
	var entries []catalogSearch
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func writeCatalog(root string, entries []catalogSearch) error {
	path := filepath.Join(root, "Data", "searches.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".searches-*.tmp")
	if err != nil {
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func serveProjectFile(root string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), string(filepath.Separator))
	if name == "" || name == "." {
		name = "index.html"
	}
	if name != "index.html" && name != "page.js" && name != "Data/searches.json" && !strings.HasPrefix(name, "CSV/") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(root, filepath.FromSlash(name)))
}

func uploadFileName(part *multipart.Part) string {
	return filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
}

func inputFiles(args []string) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}
	files, err := filepath.Glob(filepath.Join("..", "RCN", "*.gml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no GML files found in ../RCN; pass input files or add RCN exports")
	}
	return files, nil
}

func readFeatures(paths []string) ([]feature, map[string]feature, error) {
	var all []feature
	byID := make(map[string]feature)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, nil, fmt.Errorf("open %s: %w", path, err)
		}
		count, parseErr := decodeFeatures(file, filepath.Base(path), func(f feature) {
			all = append(all, f)
			if f.ID != "" {
				byID[f.ID] = f
			}
		})
		closeErr := file.Close()
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, parseErr)
		}
		if closeErr != nil {
			return nil, nil, fmt.Errorf("close %s: %w", path, closeErr)
		}
		fmt.Fprintf(os.Stderr, "%s: %d RCN features\n", filepath.Base(path), count)
	}
	if len(all) == 0 {
		return nil, nil, errors.New("no RCN features found in input")
	}
	return all, byID, nil
}

func decodeFeatures(r io.Reader, source string, emit func(feature)) (int, error) {
	decoder := xml.NewDecoder(r)
	decoder.Strict = true
	count := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.HasPrefix(start.Name.Local, "RCN_") {
			continue
		}
		var node xmlNode
		if err := decoder.DecodeElement(&node, &start); err != nil {
			return count, err
		}
		f := feature{Type: node.XMLName.Local, Source: source, Fields: make(map[string]string)}
		for _, attr := range node.Attrs {
			if attr.Name.Local == "id" {
				f.ID = stripID(attr.Value)
				break
			}
		}
		walkNode(node, &f)
		emit(f)
		count++
	}
}

func walkNode(node xmlNode, f *feature) {
	if node.XMLName.Local == "posList" && len(f.Points) == 0 {
		values := strings.Fields(node.Text)
		for i := 0; i+1 < len(values); i += 2 {
			northing, errNorth := strconv.ParseFloat(values[i], 64)
			easting, errEast := strconv.ParseFloat(values[i+1], 64)
			if errNorth == nil && errEast == nil {
				// RCN EPSG:2177 posList ordinates are northing, easting.
				f.Points = append(f.Points, point{X: easting, Y: northing})
			}
		}
	}
	if node.Text != "" && node.XMLName.Local != "" {
		if _, exists := f.Fields[node.XMLName.Local]; !exists {
			f.Fields[node.XMLName.Local] = strings.TrimSpace(node.Text)
		}
	}
	for _, attr := range node.Attrs {
		if attr.Name.Local == "href" {
			f.Refs = append(f.Refs, reference{Kind: node.XMLName.Local, ID: stripID(attr.Value)})
		}
	}
	for _, child := range node.Nodes {
		walkNode(child, f)
	}
}

func stripID(id string) string {
	id = strings.TrimPrefix(id, "#")
	return timestampSuffix.ReplaceAllString(id, "")
}

func runRadius(args []string) error {
	flags := flag.NewFlagSet("radius", flag.ContinueOnError)
	parcelID := flags.String("parcel", "306401_1.0050.AR_02.2/38", "reference parcel identifier")
	centerArgument := flags.String("center", "", "optional center as latitude,longitude (WGS84)")
	radius := flags.Float64("radius", 500, "search radius in metres")
	output := flags.String("output", filepath.Join("..", "CSV", "okolica_2-38_500m"), "output file prefix")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *radius < 0 || math.IsNaN(*radius) || math.IsInf(*radius, 0) {
		return fmt.Errorf("radius must be a finite non-negative number")
	}
	files, err := inputFiles(flags.Args())
	if err != nil {
		return err
	}
	all, byID, err := readFeatures(files)
	if err != nil {
		return err
	}

	var center point
	if *centerArgument != "" {
		var ok bool
		center, ok, err = parseCenter(*centerArgument)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("invalid center %q; expected latitude,longitude", *centerArgument)
		}
	} else {
		var seed *feature
		for i := range all {
			if all[i].Type == "RCN_Dzialka" && all[i].Fields["idDzialki"] == *parcelID {
				seed = &all[i]
				break
			}
		}
		if seed == nil {
			return fmt.Errorf("reference parcel %q was not found", *parcelID)
		}
		var ok bool
		center, ok = polygonCentroid(seed.Points)
		if !ok {
			return fmt.Errorf("reference parcel %q has no usable polygon", *parcelID)
		}
	}

	nearby := make([]nearbyFeature, 0)
	nearbyByID := make(map[string]nearbyFeature)
	for _, f := range all {
		distance := distanceToPolygon(center, f.Points)
		if distance <= *radius {
			match := nearbyFeature{Feature: f, Distance: distance}
			nearby = append(nearby, match)
			if f.ID != "" {
				nearbyByID[f.ID] = match
			}
		}
	}
	sort.Slice(nearby, func(i, j int) bool { return nearby[i].Distance < nearby[j].Distance })

	featureRows := make([]radiusFeatureRow, 0, len(nearby))
	for _, item := range nearby {
		f := item.Feature
		geometryPoints := make([][]float64, 0, len(f.Points))
		for _, p := range f.Points {
			geometryPoints = append(geometryPoints, []float64{p.X, p.Y})
		}
		geometry, _ := json.Marshal(geometryPoints)
		area := parseNumber(f.Fields["polePowierzchniEwidencyjnej"])
		price := parseNumber(f.Fields["cenaDzialkiEwidencyjnejBrutto"])
		pricePerM2 := ""
		if area > 0 && price != 0 {
			pricePerM2 = formatNumber(price / (area * 10000))
		}
		areaM2 := ""
		if area != 0 {
			areaM2 = formatNumber(area * 10000)
		}
		featureRows = append(featureRows, radiusFeatureRow{
			Type: f.Type, ID: f.ID, ParcelID: f.Fields["idDzialki"],
			Street: f.Fields["ulica"], Number: f.Fields["numerPorzadkowy"],
			Area: areaM2, Price: optionalNumber(f.Fields["cenaDzialkiEwidencyjnejBrutto"]),
			PricePerM2: pricePerM2, Geometry: string(geometry),
			Distance: formatNumber(item.Distance), Source: f.Source,
		})
	}

	transactionRows := buildRadiusTransactions(all, byID, nearbyByID)
	if err := writeCSV(*output+"-obiekty.csv", []string{
		"TypObiektu", "IdGML", "IdDzialki", "Ulica", "Numer", "PowierzchniaEwidencyjnaM2",
		"CenaDzialkiPLN", "CenaDzialkiZaM2PLN", "Geometria2177",
		"OdlegloscOdSrodkaDzialkiPunktowejM", "PlikZrodlowy",
	}, rowsFromFeatures(featureRows)); err != nil {
		return err
	}
	if err := writeCSV(*output+"-transakcje.csv", []string{
		"DataAktu", "NumerTransakcji", "RodzajTransakcji", "RodzajRynku", "CenaTransakcjiPLN",
		"PowierzchniaGruntuM2", "PowierzchniaUdzialuNabytegoM2", "Udzialy", "CenaZaM2GruntuPLN",
		"Adresy", "DzialkiPowiazane", "ObiektyWPromieniu", "OdlegloscNajblizszegoObiektuM",
		"PlikZrodlowy", "TxId",
	}, rowsFromTransactions(transactionRows)); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "reference centroid EPSG:2177 %.2f, %.2f; radius %.1f m\n", center.X, center.Y, *radius)
	fmt.Fprintf(os.Stderr, "nearby features: %d; transactions: %d\n", len(featureRows), len(transactionRows))
	return nil
}

func buildRadiusTransactions(all []feature, byID map[string]feature, nearby map[string]nearbyFeature) []radiusTransactionRow {
	rows := make([]radiusTransactionRow, 0)
	seen := make(map[string]bool)
	for _, tx := range all {
		if tx.Type != "RCN_Transakcja" {
			continue
		}
		var relatedParcels, shares, matchedIDs, addresses []string
		var areaTotal, acquiredTotal float64
		var areaCount, acquiredCount int
		minimumDistance := math.Inf(1)
		for _, ref := range tx.Refs {
			if ref.Kind != "nieruchomosc" {
				continue
			}
			property, ok := byID[ref.ID]
			if !ok {
				continue
			}
			areaText := property.Fields["polePowierzchniNieruchomosciGruntowej"]
			shareText := property.Fields["udzialWPrawieDoNieruchomosci"]
			if shareText != "" && !contains(shares, shareText) {
				shares = append(shares, shareText)
			}
			if areaText != "" {
				areaM2 := parseNumber(areaText) * 10000
				areaTotal += areaM2
				areaCount++
				if numerator, denominator, ok := parseShare(shareText); ok {
					acquiredTotal += areaM2 * numerator / denominator
					acquiredCount++
				}
			}
			for _, assetRef := range property.Refs {
				switch assetRef.Kind {
				case "dzialka", "budynek", "lokal":
					if item, exists := nearby[assetRef.ID]; exists {
						if !contains(matchedIDs, item.Feature.Type+":"+item.Feature.ID) {
							matchedIDs = append(matchedIDs, item.Feature.Type+":"+item.Feature.ID)
						}
						minimumDistance = math.Min(minimumDistance, item.Distance)
						street := item.Feature.Fields["ulica"]
						if street != "" {
							address := street
							if number := item.Feature.Fields["numerPorzadkowy"]; number != "" {
								address += " " + number
							}
							if !contains(addresses, address) {
								addresses = append(addresses, address)
							}
						}
					}
					if asset, exists := byID[assetRef.ID]; exists && asset.Type == "RCN_Dzialka" {
						if parcelID := asset.Fields["idDzialki"]; parcelID != "" && !contains(relatedParcels, parcelID) {
							relatedParcels = append(relatedParcels, parcelID)
						}
					}
				}
			}
		}
		if len(matchedIDs) == 0 || seen[tx.ID] {
			continue
		}
		seen[tx.ID] = true
		document := referenced(tx, "podstawaPrawna", byID)
		price := parseNumber(tx.Fields["cenaTransakcjiBrutto"])
		pricePerM2 := ""
		if areaCount > 0 && acquiredCount == areaCount && acquiredTotal > 0 {
			pricePerM2 = formatNumber(math.Round(price/acquiredTotal*100) / 100)
		}
		areaValue, acquiredValue, distanceValue := "", "", ""
		if areaCount > 0 {
			areaValue = formatNumber(math.Round(areaTotal*100) / 100)
		}
		if acquiredCount == areaCount && acquiredCount > 0 {
			acquiredValue = formatNumber(math.Round(acquiredTotal*100) / 100)
		}
		if !math.IsInf(minimumDistance, 1) {
			distanceValue = formatNumber(math.Round(minimumDistance*10) / 10)
		}
		rows = append(rows, radiusTransactionRow{
			Date:   field(document, "dataSporzadzeniaDokumentu"),
			Number: tx.Fields["oznaczenieTransakcji"],
			Kind:   tx.Fields["rodzajTransakcji"], Market: tx.Fields["rodzajRynku"],
			Price: optionalNumber(tx.Fields["cenaTransakcjiBrutto"]), Area: areaValue,
			ShareArea: acquiredValue, Shares: strings.Join(shares, "; "),
			PricePerM2: pricePerM2, Addresses: strings.Join(addresses, "; "),
			ParcelIDs: strings.Join(relatedParcels, "; "), Matched: strings.Join(matchedIDs, "; "),
			Distance: distanceValue, Source: tx.Source, ID: tx.ID,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date == rows[j].Date {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Date < rows[j].Date
	})
	return rows
}

func rowsFromFeatures(rows []radiusFeatureRow) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, []string{row.Type, row.ID, row.ParcelID, row.Street, row.Number, row.Area, row.Price, row.PricePerM2, row.Geometry, row.Distance, row.Source})
	}
	return out
}

func rowsFromTransactions(rows []radiusTransactionRow) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, []string{row.Date, row.Number, row.Kind, row.Market, row.Price, row.Area, row.ShareArea, row.Shares, row.PricePerM2, row.Addresses, row.ParcelIDs, row.Matched, row.Distance, row.Source, row.ID})
	}
	return out
}

func writeCSV(path string, header []string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	writer := csv.NewWriter(file)
	writer.Comma = ';'
	writeErr := writer.Write(header)
	if writeErr == nil {
		for _, row := range rows {
			if writeErr = writer.Write(row); writeErr != nil {
				break
			}
		}
	}
	writer.Flush()
	if writeErr == nil {
		writeErr = writer.Error()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	return nil
}

func polygonCentroid(points []point) (point, bool) {
	if len(points) < 3 {
		return point{}, false
	}
	var area2, cx, cy float64
	for i := 0; i+1 < len(points); i++ {
		cross := points[i].X*points[i+1].Y - points[i+1].X*points[i].Y
		area2 += cross
		cx += (points[i].X + points[i+1].X) * cross
		cy += (points[i].Y + points[i+1].Y) * cross
	}
	if math.Abs(area2) < 0.001 {
		var x, y float64
		for _, p := range points[:len(points)-1] {
			x += p.X
			y += p.Y
		}
		n := float64(len(points) - 1)
		return point{X: x / n, Y: y / n}, true
	}
	return point{X: cx / (3 * area2), Y: cy / (3 * area2)}, true
}

func parseCenter(value string) (point, bool, error) {
	coordinates := strings.Split(value, ",")
	if len(coordinates) != 2 {
		return point{}, false, nil
	}
	latitude, errLatitude := strconv.ParseFloat(strings.TrimSpace(coordinates[0]), 64)
	longitude, errLongitude := strconv.ParseFloat(strings.TrimSpace(coordinates[1]), 64)
	if errLatitude != nil || errLongitude != nil || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return point{}, false, nil
	}
	return epsg2177(latitude, longitude), true, nil
}

func epsg2177(latitude, longitude float64) point {
	const (
		semiMajor       = 6378137.0
		flattening      = 1 / 298.257222101
		scale           = 0.999923
		centralMeridian = 18.0
		falseEasting    = 6500000.0
	)
	phi := latitude * math.Pi / 180
	lambda := longitude * math.Pi / 180
	lambda0 := centralMeridian * math.Pi / 180
	eccentricity2 := flattening * (2 - flattening)
	eccentricity4 := eccentricity2 * eccentricity2
	eccentricity6 := eccentricity4 * eccentricity2
	secondEccentricity2 := eccentricity2 / (1 - eccentricity2)
	sinPhi, cosPhi := math.Sin(phi), math.Cos(phi)
	tanPhi := math.Tan(phi)
	n := semiMajor / math.Sqrt(1-eccentricity2*sinPhi*sinPhi)
	t := tanPhi * tanPhi
	c := secondEccentricity2 * cosPhi * cosPhi
	a := cosPhi * (lambda - lambda0)

	meridian := semiMajor * ((1-eccentricity2/4-3*eccentricity4/64-5*eccentricity6/256)*phi -
		(3*eccentricity2/8+3*eccentricity4/32+45*eccentricity6/1024)*math.Sin(2*phi) +
		(15*eccentricity4/256+45*eccentricity6/1024)*math.Sin(4*phi) -
		(35*eccentricity6/3072)*math.Sin(6*phi))
	x := falseEasting + scale*n*(a+(1-t+c)*math.Pow(a, 3)/6+(5-18*t+t*t+72*c-58*secondEccentricity2)*math.Pow(a, 5)/120)
	y := scale * (meridian + n*tanPhi*(a*a/2+(5-t+9*c+4*c*c)*math.Pow(a, 4)/24+(61-58*t+t*t+600*c-330*secondEccentricity2)*math.Pow(a, 6)/720))
	return point{X: x, Y: y}
}

func distanceToPolygon(p point, polygon []point) float64 {
	if len(polygon) < 3 {
		return math.Inf(1)
	}
	inside := false
	j := len(polygon) - 1
	minimum := math.Inf(1)
	for i := range polygon {
		a, b := polygon[i], polygon[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			inside = !inside
		}
		if i+1 < len(polygon) {
			minimum = math.Min(minimum, distanceToSegment(p, polygon[i], polygon[i+1]))
		}
		j = i
	}
	if inside {
		return 0
	}
	return minimum
}

func distanceToSegment(p, a, b point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	length2 := dx*dx + dy*dy
	if length2 == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / length2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

func parseNumber(text string) float64 {
	value, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), 64)
	return value
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func optionalNumber(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return formatNumber(parseNumber(text))
}

func runStreets(args []string) error {
	flags := flag.NewFlagSet("streets", flag.ContinueOnError)
	streetList := flags.String("streets", "Pszczelna,Zagonowa,Winna,Warzywna,Pasieka,Sadowa,Gronowa,Wyżyny,Winogrady", "comma-separated street-name fragments")
	output := flags.String("output", filepath.Join("..", "CSV", "stare_winogrady"), "output file prefix")
	if err := flags.Parse(args); err != nil {
		return err
	}
	names := splitNonEmpty(*streetList, ",")
	if len(names) == 0 {
		return errors.New("provide at least one street name")
	}
	files, err := inputFiles(flags.Args())
	if err != nil {
		return err
	}
	all, byID, err := readFeatures(files)
	if err != nil {
		return err
	}
	rows := buildStreetRows(all, byID, names)
	if err := writeCSV(*output+".csv", streetHeaders, rows); err != nil {
		return err
	}
	jsonPath := *output + ".json"
	if err := writeJSON(jsonPath, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "matching transactions: %d\n", len(rows))
	return nil
}

var streetHeaders = []string{
	"data_aktu", "notariusz", "numer_transakcji", "rodzaj_transakcji", "rodzaj_rynku",
	"cena_transakcji_brutto", "rodzaj_nieruchomosci", "prawo_do_nieruchomosci",
	"udzial_w_prawie", "pow_gruntowa_ha", "ulica", "numer", "rodzaj_budynku",
	"pow_budynku_m2", "id_dzialki", "przeznaczenie_mpzp", "pow_ewidencyjna_ha",
	"pow_lokalu_m2", "liczba_izb", "kondygnacja", "funkcja_lokalu", "plik",
}

func buildStreetRows(all []feature, byID map[string]feature, names []string) [][]string {
	var rows [][]string
	for _, tx := range all {
		if tx.Type != "RCN_Transakcja" {
			continue
		}
		property := referenced(tx, "nieruchomosc", byID)
		if property == nil {
			continue
		}
		var buildings, parcels, units []feature
		for _, ref := range property.Refs {
			switch ref.Kind {
			case "budynek":
				if f := byID[ref.ID]; f.Type != "" {
					buildings = append(buildings, f)
				}
			case "dzialka":
				if f := byID[ref.ID]; f.Type != "" {
					parcels = append(parcels, f)
				}
			case "lokal":
				if f := byID[ref.ID]; f.Type != "" {
					units = append(units, f)
				}
			}
		}
		addresses := append(append(append([]feature{}, buildings...), units...), parcels...)
		matches := false
		for _, address := range addresses {
			for _, name := range names {
				if strings.Contains(strings.ToLower(address.Fields["ulica"]), strings.ToLower(name)) {
					matches = true
				}
			}
		}
		if !matches {
			continue
		}
		document := referenced(tx, "podstawaPrawna", byID)
		firstBuilding := first(buildings)
		firstParcel := first(parcels)
		firstUnit := first(units)
		street, number, buildingType, buildingArea := "", "", "", ""
		if firstBuilding != nil {
			street, number = firstBuilding.Fields["ulica"], firstBuilding.Fields["numerPorzadkowy"]
			buildingType = mapCode(firstBuilding.Fields["rodzajBudynku"], buildingTypeCodes)
			buildingArea = firstBuilding.Fields["powierzchniaUzytkowaBudynku"]
		} else if firstUnit != nil {
			street, number = firstUnit.Fields["ulica"], firstUnit.Fields["numerPorzadkowy"]
		} else if firstParcel != nil {
			street, number = firstParcel.Fields["ulica"], firstParcel.Fields["numerPorzadkowy"]
		}
		unitArea, rooms, floor, unitKind := "", "", "", ""
		if firstUnit != nil {
			unitArea = firstUnit.Fields["powUzytkowaLokalu"]
			rooms = firstUnit.Fields["liczbaIzb"]
			floor = firstUnit.Fields["nrKondygnacji"]
			unitKind = mapCode(firstUnit.Fields["funkcjaLokalu"], unitCodes)
		}
		row := []string{
			field(document, "dataSporzadzeniaDokumentu"),
			field(document, "tworcaDokumentu"),
			tx.Fields["oznaczenieTransakcji"],
			mapCode(tx.Fields["rodzajTransakcji"], transactionCodes),
			mapCode(tx.Fields["rodzajRynku"], marketCodes),
			tx.Fields["cenaTransakcjiBrutto"],
			mapCode(property.Fields["rodzajNieruchomosci"], propertyTypeCodes),
			mapCode(property.Fields["rodzajPrawaDoNieruchomosci"], rightsCodes),
			property.Fields["udzialWPrawieDoNieruchomosci"],
			property.Fields["polePowierzchniNieruchomosciGruntowej"],
			street, number, buildingType, buildingArea,
			field(firstParcel, "idDzialki"),
			field(firstParcel, "przeznaczenieWMPZP"),
			field(firstParcel, "polePowierzchniEwidencyjnej"),
			unitArea, rooms, floor, unitKind, tx.Source,
		}
		rows = append(rows, row)
	}
	return rows
}

func referenced(f feature, kind string, byID map[string]feature) *feature {
	for _, ref := range f.Refs {
		if ref.Kind == kind {
			if item, ok := byID[ref.ID]; ok {
				return &item
			}
		}
	}
	return nil
}

func first(features []feature) *feature {
	if len(features) == 0 {
		return nil
	}
	return &features[0]
}

func field(f *feature, name string) string {
	if f == nil {
		return ""
	}
	return f.Fields[name]
}

func splitNonEmpty(value, separator string) []string {
	var out []string
	for _, part := range strings.Split(value, separator) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func writeJSON(path string, rows [][]string) error {
	records := make([]map[string]string, 0, len(rows))
	for _, values := range rows {
		record := make(map[string]string, len(streetHeaders))
		for i, header := range streetHeaders {
			record[header] = values[i]
		}
		records = append(records, record)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func mapCode(code string, values map[string]string) string {
	if value, ok := values[code]; ok {
		return value
	}
	return code
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func parseShare(value string) (float64, float64, bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	numerator, errNumerator := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	denominator, errDenominator := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	return numerator, denominator, errNumerator == nil && errDenominator == nil && denominator > 0
}

var (
	transactionCodes  = map[string]string{"1": "sprzedaz", "2": "zamiana", "3": "inna"}
	marketCodes       = map[string]string{"1": "pierwotny", "2": "wtorny"}
	propertyTypeCodes = map[string]string{"1": "niezabudowana", "2": "zabudowana", "3": "budynkowa", "4": "lokalowa"}
	rightsCodes       = map[string]string{"1": "wlasnosc", "2": "uzytkowanie_wieczyste", "3": "wlasnosc_lokalu_SM", "4": "inne"}
	unitCodes         = map[string]string{"1": "mieszkalny", "2": "uzytkowy", "3": "garaza"}
	buildingTypeCodes = map[string]string{
		"100": "niemieszkalne", "101": "przemyslowe", "102": "magazynowe", "103": "handlowe",
		"104": "biurowe", "105": "transportu", "106": "kultura", "107": "oswiata",
		"108": "lecznictwo", "109": "goscinne", "110": "mieszkalne_jedno", "111": "mieszkalne_wielo",
		"112": "zbiorowe", "113": "rekreacji", "114": "inne_niemieszkalne",
	}
)
