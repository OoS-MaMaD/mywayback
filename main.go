package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultExclude = "js,css,png,jpg,jpeg,gif,svg,webp,ico,bmp,tif,tiff,woff,woff2,ttf,eot,mp4,mp3,wav,avi,mov,mkv,zip,rar,7z,pdf"

func main() {
	// Flags
	urlFlag := flag.String("u", "", "Target URL pattern (e.g. *.example.com)")
	outputFile := flag.String("o", "", "Output file (also prints to stdout)")
	onlyQuery := flag.Bool("only-query", false, "Output only full query strings")
	onlyQueryKeys := flag.Bool("only-query-keys", false, "Output only query parameter keys")
	noQuery := flag.Bool("no-query", false, "Remove query strings from URLs")
	excludeExt := flag.String("exclude-ext", defaultExclude, "Comma-separated list of extensions to exclude")
	includeExt := flag.String("include-ext", "", "Comma-separated list of extensions to include (overrides exclude)")
	workers := flag.Int("workers", 20, "Number of concurrent workers")
	timeout := flag.Int("timeout", 15, "HTTP timeout in seconds")
	flag.Parse()

	if *urlFlag == "" {
		fmt.Println("❌ ERROR: -u <url> is required")
		flag.Usage()
		os.Exit(1)
	}

	// Build CDX URL
	cdxURL := "https://web.archive.org/cdx/search/cdx?url=" +
		url.QueryEscape(normalizeURL(*urlFlag)) +
		"&fl=original&collapse=urlkey"

	// Fetch CDX
	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	resp, err := client.Get(cdxURL)
	if err != nil {
		fmt.Println("❌ ERROR fetching CDX:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	lines := []string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("❌ ERROR reading CDX:", err)
		os.Exit(1)
	}

	// Compile extension filters
	extRegex, includeMode, err := CompileExtRegex(*includeExt, *excludeExt)
	if err != nil {
		fmt.Println("❌ ERROR compiling extension regex:", err)
		os.Exit(1)
	}

	// Process URLs concurrently
	results := processConcurrently(lines, *workers, extRegex, includeMode, *onlyQuery, *onlyQueryKeys, *noQuery)

	// Deduplicate & sort
	final := uniqueAndSort(results)

	// Output
	if *outputFile != "" {
		f, err := os.Create(*outputFile)
		if err != nil {
			fmt.Println("❌ ERROR creating output file:", err)
			os.Exit(1)
		}
		defer f.Close()
		w := bufio.NewWriter(io.MultiWriter(os.Stdout, f))
		for _, l := range final {
			fmt.Fprintln(w, l)
		}
		w.Flush()
		fmt.Println("✔ Saved results to", *outputFile)
	} else {
		for _, l := range final {
			fmt.Println(l)
		}
	}
}

// normalizeURL ensures the pattern ends with * if missing
func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	if !strings.Contains(u, "*") {
		u += "*"
	}
	return u
}

// processConcurrently filters and processes URLs with a worker pool
func processConcurrently(lines []string, workers int, extRegex *regexp.Regexp, includeMode bool, onlyQuery, onlyQueryKeys, noQuery bool) []string {
	jobs := make(chan string)
	results := make(chan string, len(lines))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for line := range jobs {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				// parse URL to get path only for extension check
				u, err := url.Parse(line)
				path := line
				if err == nil && u.Path != "" {
					path = u.Path
				}

				// extension filtering
				if extRegex != nil {
					match := extRegex.MatchString(path)
					if includeMode && !match {
						continue
					} else if !includeMode && match {
						continue
					}
				}

				// query modes
				if onlyQuery {
					if err == nil && u.RawQuery != "" {
						results <- u.RawQuery
					}
					continue
				}
				if onlyQueryKeys {
					if err == nil && u.RawQuery != "" {
						keys := []string{}
						for k := range u.Query() {
							keys = append(keys, k)
						}
						sort.Strings(keys)
						results <- strings.Join(keys, "&")
					}
					continue
				}
				if noQuery && err == nil {
					u.RawQuery = ""
					results <- u.String()
					continue
				}

				// default: send the original URL
				results <- line
			}
		}()
	}

	go func() {
		for _, l := range lines {
			jobs <- l
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	out := []string{}
	for r := range results {
		out = append(out, r)
	}
	return out
}
