package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// ---------- Config ----------

type App struct {
	Name        string `json:"name"`
	Source      string `json:"source"` // "github" or "farsroid"
	Repo        string `json:"repo,omitempty"`
	URL         string `json:"url,omitempty"`
	LastVersion string `json:"last_version"`
	LastLink    string `json:"last_link"`
}

type Config struct {
	TelegramToken string `json:"telegram_token"`
	ChatID        string `json:"chat_id"`
	Apps          []App  `json:"apps"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ---------- HTTP helper ----------

var httpClient = &http.Client{Timeout: 20 * time.Second}

func fetch(u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	// Farsroid (and some CDNs) block requests with no browser-like User-Agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d for %s", resp.StatusCode, u)
	}
	return io.ReadAll(resp.Body)
}

// ---------- GitHub ----------

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
	HTMLURL string `json:"html_url"`
}

func checkGithub(repo string) (version, link string, err error) {
	api := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	body, err := fetch(api)
	if err != nil {
		return "", "", err
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", err
	}
	version = rel.TagName

	// Prefer an arm64 apk asset, then any apk asset, then the release page itself.
	best := ""
	for _, a := range rel.Assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".apk") && strings.Contains(lower, "arm64") {
			best = a.BrowserDownloadURL
			break
		}
	}
	if best == "" {
		for _, a := range rel.Assets {
			if strings.HasSuffix(strings.ToLower(a.Name), ".apk") {
				best = a.BrowserDownloadURL
				break
			}
		}
	}
	if best == "" {
		best = rel.HTMLURL
	}
	link = best
	return version, link, nil
}

// ---------- Farsroid ----------

// NOTE: Farsroid has no public API, so this scrapes the HTML.
// The regexes below are a best-effort starting point based on the
// common Farsroid page layout (version mentioned near "نسخه", direct
// download buttons pointing at their CDN). If a specific app's page
// doesn't match, tweak these two patterns first - everything else in
// the program stays the same.
var (
	farsroidVersionRe = regexp.MustCompile(`نسخه[\s:\-]*([0-9]+(?:\.[0-9]+){1,3}[a-zA-Z0-9]*)`)
	// Matches <a href="...">...دانلود...</a> style buttons, capturing the href.
	farsroidLinkRe = regexp.MustCompile(`(?i)<a[^>]+href="([^"]+)"[^>]*>[^<]*دانلود[^<]*</a>`)
)

func checkFarsroid(pageURL string) (version, link string, err error) {
	body, err := fetch(pageURL)
	if err != nil {
		return "", "", err
	}
	html := string(body)

	if m := farsroidVersionRe.FindStringSubmatch(html); m != nil {
		version = m[1]
	} else {
		return "", "", fmt.Errorf("version pattern not found on %s (site layout may have changed - adjust farsroidVersionRe)", pageURL)
	}

	matches := farsroidLinkRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return "", "", fmt.Errorf("no download link found on %s (adjust farsroidLinkRe)", pageURL)
	}
	// Prefer a link that mentions arm64 if there are multiple architecture variants.
	best := matches[0][1]
	for _, m := range matches {
		if strings.Contains(strings.ToLower(m[1]), "arm64") {
			best = m[1]
			break
		}
	}
	link = best
	return version, link, nil
}

// ---------- Telegram ----------

func sendTelegram(token, chatID, text string) error {
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", text)
	form.Set("disable_web_page_preview", "false")
	resp, err := httpClient.PostForm(api, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ---------- Main ----------

func main() {
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("failed to load config %s: %v", configPath, err)
	}

	changed := false

	for i := range cfg.Apps {
		app := &cfg.Apps[i]

		var version, link string
		var checkErr error

		switch app.Source {
		case "github":
			version, link, checkErr = checkGithub(app.Repo)
		case "farsroid":
			version, link, checkErr = checkFarsroid(app.URL)
		default:
			log.Printf("skip %s: unknown source %q", app.Name, app.Source)
			continue
		}

		if checkErr != nil {
			log.Printf("[%s] check failed: %v", app.Name, checkErr)
			continue
		}

		switch {
		case app.LastVersion == "":
			// First run for this app: just set the baseline, no notification.
			log.Printf("[%s] baseline set to %s", app.Name, version)
			app.LastVersion = version
			app.LastLink = link
			changed = true

		case version != app.LastVersion:
			log.Printf("[%s] update found: %s -> %s", app.Name, app.LastVersion, version)
			msg := fmt.Sprintf("🔔 %s\nنسخه جدید: %s\nلینک: %s", app.Name, version, link)
			if err := sendTelegram(cfg.TelegramToken, cfg.ChatID, msg); err != nil {
				log.Printf("[%s] telegram send failed: %v", app.Name, err)
				continue // don't update state if notification failed, so it retries next run
			}
			app.LastVersion = version
			app.LastLink = link
			changed = true

		default:
			log.Printf("[%s] no change (%s)", app.Name, version)
		}

		// Be polite to GitHub's unauthenticated rate limit and to Farsroid.
		time.Sleep(2 * time.Second)
	}

	if changed {
		if err := saveConfig(configPath, cfg); err != nil {
			log.Fatalf("failed to save config: %v", err)
		}
	}
}
