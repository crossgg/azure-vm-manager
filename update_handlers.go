package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var version = "dev"

const (
	defaultUpdateRepo = "crossgg/cloud-vm-manager"
	runtimeBinPath    = "/app/runtime/cloud-vm-manager"
	updateTempDir     = "/app/runtime/update"
)

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Name    string               `json:"name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type updateInfo struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	AssetName       string `json:"assetName,omitempty"`
	RuntimePath     string `json:"runtimePath"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	DownloadProxy   string `json:"downloadProxy,omitempty"`
	CheckError      string `json:"checkError,omitempty"`
}

func getUpdateStatus(c *gin.Context) {
	info := updateInfo{
		CurrentVersion: version,
		RuntimePath:    runtimeBinPath,
		DownloadProxy:  defaultDownloadProxy(),
	}

	if c.Query("check") == "true" {
		release, asset, err := latestReleaseAsset(c.Query("download_proxy"))
		if err != nil {
			info.CheckError = err.Error()
		} else {
			info.LatestVersion = release.TagName
			info.UpdateAvailable = version == "dev" || release.TagName != version
			info.AssetName = asset.Name
			info.ReleaseURL = release.HTMLURL
		}
	}

	c.JSON(http.StatusOK, info)
}

func applyUpdate(c *gin.Context) {
	var payload struct {
		DownloadProxy string `json:"downloadProxy"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil && err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	release, asset, err := latestReleaseAsset(payload.DownloadProxy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := installReleaseAsset(release, asset, payload.DownloadProxy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"message":       "update installed; restarting process",
		"latestVersion": release.TagName,
		"assetName":     asset.Name,
	})

	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
}

func latestReleaseAsset(downloadProxy string) (githubRelease, githubReleaseAsset, error) {
	release, err := fetchLatestRelease(downloadProxy)
	if err != nil {
		return githubRelease{}, githubReleaseAsset{}, err
	}
	assetName := updateAssetName(runtime.GOOS, runtime.GOARCH, os.Getenv("GOARM"))
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return release, asset, nil
		}
	}
	return githubRelease{}, githubReleaseAsset{}, fmt.Errorf("release %s has no asset %s", release.TagName, assetName)
}

func fetchLatestRelease(downloadProxy string) (githubRelease, error) {
	repo := strings.TrimSpace(os.Getenv("UPDATE_GITHUB_REPO"))
	if repo == "" {
		repo = defaultUpdateRepo
	}
	apiProxy := strings.TrimSpace(downloadProxy)
	if apiProxy == "https://gh-proxy.com/" || apiProxy == "https://gh-proxy.com" || apiProxy == defaultDownloadProxy() {
		apiProxy = ""
	}
	endpoint := proxiedDownloadURL(fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo), apiProxy)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cloud-vm-manager-updater")

	token := strings.TrimSpace(os.Getenv("UPDATE_GITHUB_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return githubRelease{}, fmt.Errorf("GitHub release API returned %d: %s", resp.StatusCode, string(body))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	return release, nil
}

func updateAssetName(goos, goarch, goarm string) string {
	switch {
	case goos == "windows" && goarch == "amd64":
		return "cloud-vm-manager_windows_amd64.zip"
	case goos == "linux" && goarch == "amd64":
		return "cloud-vm-manager_linux_amd64.tar.gz"
	case goos == "linux" && goarch == "arm64":
		return "cloud-vm-manager_linux_arm64.tar.gz"
	case goos == "linux" && goarch == "arm":
		return "cloud-vm-manager_linux_armv7.tar.gz"
	default:
		return fmt.Sprintf("cloud-vm-manager_%s_%s.tar.gz", goos, goarch)
	}
}

func installReleaseAsset(release githubRelease, asset githubReleaseAsset, downloadProxy string) error {
	if asset.BrowserDownloadURL == "" {
		return fmt.Errorf("release asset %s has no download URL", asset.Name)
	}
	if err := os.MkdirAll(updateTempDir, 0o755); err != nil {
		return err
	}
	archivePath := filepath.Join(updateTempDir, asset.Name)
	if err := downloadFile(proxiedDownloadURL(asset.BrowserDownloadURL, downloadProxy), archivePath); err != nil {
		return err
	}
	if err := verifyReleaseChecksum(release, asset.Name, archivePath, downloadProxy); err != nil {
		return err
	}

	tempBinPath := filepath.Join(updateTempDir, "cloud-vm-manager")
	_ = os.Remove(tempBinPath)
	tempPublicPath := filepath.Join(updateTempDir, "public")
	_ = os.RemoveAll(tempPublicPath)

	var hasPublic bool
	var err error
	if strings.HasSuffix(asset.Name, ".zip") {
		hasPublic, err = extractArchiveFromZip(archivePath, tempBinPath, tempPublicPath)
		if err != nil {
			return err
		}
	} else {
		hasPublic, err = extractArchiveFromTarGz(archivePath, tempBinPath, tempPublicPath)
		if err != nil {
			return err
		}
	}

	if err := os.Chmod(tempBinPath, 0o755); err != nil {
		return err
	}

	var publicReplacement *directoryReplacement
	if hasPublic {
		publicReplacement, err = replaceDirectory(tempPublicPath, runtimePublicDir)
		if err != nil {
			return fmt.Errorf("install updated public dir: %w", err)
		}
	}

	if err := os.Rename(tempBinPath, runtimeBinPath); err != nil {
		if publicReplacement != nil {
			if rollbackErr := publicReplacement.rollback(); rollbackErr != nil {
				return fmt.Errorf("install runtime binary: %v; restore public dir: %w", err, rollbackErr)
			}
		}
		return fmt.Errorf("install runtime binary: %w", err)
	}
	if publicReplacement != nil {
		_ = publicReplacement.commit()
	}

	return nil
}

type directoryReplacement struct {
	target    string
	backup    string
	hadTarget bool
}

func replaceDirectory(source, target string) (*directoryReplacement, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source %s is not a directory", source)
	}
	if filepath.Clean(source) == filepath.Clean(target) {
		return nil, fmt.Errorf("source and target directories must differ")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}

	replacement := &directoryReplacement{
		target: target,
		backup: target + ".previous",
	}
	if err := os.RemoveAll(replacement.backup); err != nil {
		return nil, err
	}
	if _, err := os.Stat(target); err == nil {
		replacement.hadTarget = true
		if err := os.Rename(target, replacement.backup); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.Rename(source, target); err != nil {
		if replacement.hadTarget {
			if restoreErr := os.Rename(replacement.backup, target); restoreErr != nil {
				return nil, fmt.Errorf("replace directory: %v; restore previous directory: %w", err, restoreErr)
			}
		}
		return nil, err
	}
	return replacement, nil
}

func (r *directoryReplacement) rollback() error {
	if r == nil {
		return nil
	}
	if err := os.RemoveAll(r.target); err != nil {
		return err
	}
	if r.hadTarget {
		return os.Rename(r.backup, r.target)
	}
	return nil
}

func (r *directoryReplacement) commit() error {
	if r == nil || !r.hadTarget {
		return nil
	}
	return os.RemoveAll(r.backup)
}

func downloadFile(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "cloud-vm-manager-updater")
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download returned %d: %s", resp.StatusCode, string(body))
	}

	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func verifyReleaseChecksum(release githubRelease, assetName, archivePath string, downloadProxy string) error {
	var checksumAsset *githubReleaseAsset
	for i := range release.Assets {
		if release.Assets[i].Name == "checksums.txt" {
			checksumAsset = &release.Assets[i]
			break
		}
	}
	if checksumAsset == nil {
		return fmt.Errorf("release %s has no checksums.txt", release.TagName)
	}

	checksumPath := filepath.Join(updateTempDir, "checksums.txt")
	if err := downloadFile(proxiedDownloadURL(checksumAsset.BrowserDownloadURL, downloadProxy), checksumPath); err != nil {
		return err
	}
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return err
	}
	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksums.txt does not contain %s", assetName)
	}

	actual, err := fileSHA256(archivePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

func defaultDownloadProxy() string {
	runtimeState.mu.RLock()
	cfg := runtimeState.cfg
	runtimeState.mu.RUnlock()
	if cfg != nil && cfg.Update.DownloadProxy != "" {
		return cfg.Update.DownloadProxy
	}
	return strings.TrimSpace(os.Getenv("UPDATE_DOWNLOAD_PROXY"))
}

func proxiedDownloadURL(rawURL, proxy string) string {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return rawURL
	}
	if !strings.HasSuffix(proxy, "/") {
		proxy += "/"
	}
	return proxy + rawURL
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractArchiveFromZip(archivePath, destBin, destPublicDir string) (bool, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return false, err
	}
	defer reader.Close()

	binExtracted := false
	hasPublic := false
	for _, file := range reader.File {
		name := strings.TrimPrefix(file.Name, "./")
		baseName := filepath.Base(name)

		if (baseName == "cloud-vm-manager" || baseName == "cloud-vm-manager.exe") && !file.FileInfo().IsDir() {
			rc, err := file.Open()
			if err != nil {
				return false, err
			}
			err = writeFileFromReader(destBin, rc)
			rc.Close()
			if err != nil {
				return false, err
			}
			binExtracted = true
		} else if (strings.HasPrefix(name, "public/") || strings.Contains(name, "/public/")) && !file.FileInfo().IsDir() {
			pubPath := name
			if idx := strings.Index(name, "public/"); idx != -1 {
				pubPath = name[idx:]
			}
			rc, err := file.Open()
			if err != nil {
				return false, err
			}
			targetPath := filepath.Join(destPublicDir, strings.TrimPrefix(pubPath, "public/"))
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				rc.Close()
				return false, err
			}
			err = writeFileFromReader(targetPath, rc)
			rc.Close()
			if err != nil {
				return false, err
			}
			hasPublic = true
		}
	}
	if !binExtracted {
		return false, fmt.Errorf("archive does not contain cloud-vm-manager binary")
	}
	return hasPublic, nil
}

func extractArchiveFromTarGz(archivePath, destBin, destPublicDir string) (bool, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return false, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return false, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	binExtracted := false
	hasPublic := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, err
		}

		name := header.Name
		name = strings.TrimPrefix(name, "./")

		if filepath.Base(name) == "cloud-vm-manager" && header.Typeflag == tar.TypeReg {
			if err := writeFileFromReader(destBin, tr); err != nil {
				return false, err
			}
			binExtracted = true
		} else if (strings.HasPrefix(name, "public/") || strings.Contains(name, "/public/")) && header.Typeflag == tar.TypeReg {
			pubPath := name
			if idx := strings.Index(name, "public/"); idx != -1 {
				pubPath = name[idx:]
			}
			targetPath := filepath.Join(destPublicDir, strings.TrimPrefix(pubPath, "public/"))
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return false, err
			}
			if err := writeFileFromReader(targetPath, tr); err != nil {
				return false, err
			}
			hasPublic = true
		}
	}
	if !binExtracted {
		return false, fmt.Errorf("archive does not contain cloud-vm-manager binary")
	}
	return hasPublic, nil
}

func writeFileFromReader(dest string, reader io.Reader) error {
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
