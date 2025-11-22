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
	"strconv"
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

	// Create HTTP client
	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}

	// First request: get number of pages
	pagesURL := "http://web.archive.org/cdx/search/cdx?url=" + url.QueryEscape(normalizeURL(*urlFlag)) + "&showNumPages=true"
	resp, err := client.Get(pagesURL)
	if err != nil {
		fmt.Println("❌ ERROR fetching page count from CDX:", err)
		os.Exit(1)
	}
	scanner := bufio.NewScanner(resp.Body)
	numStr := ""
	for scanner.Scan() {
		s := strings.TrimSpace(scanner.Text())
		if s != "" {
			numStr = s
			break
		}
	}
	resp.Body.Close()
	if err := scanner.Err(); err != nil {
		fmt.Println("❌ ERROR reading page-count response:", err)
		os.Exit(1)
	}

	pages := 0
	if numStr == "" {
		// no pages returned -> nothing to fetch
		pages = 0
	} else {
		if n, err := strconv.Atoi(numStr); err == nil {
			pages = n
		} else {
			// Could not parse page count; attempt to continue with a single page
			fmt.Println("⚠ WARNING: could not parse page count (", numStr, "), defaulting to 1 page")
			pages = 1
		}
	}

	// Fetch each page and collect lines
	lines := []string{}
	for p := 0; p < pages; p++ {
		pageURL := "https://web.archive.org/cdx/search/cdx?url=" + url.QueryEscape(normalizeURL(*urlFlag)) + "&page=" + strconv.Itoa(p) + "&fl=original&collapse=urlkey"
		resp, err := client.Get(pageURL)
		if err != nil {
			fmt.Println("❌ ERROR fetching CDX page", p, ":", err)
			// continue to next page
			continue
		}
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				lines = append(lines, line)
			}
		}
		if err := sc.Err(); err != nil {
			fmt.Println("⚠ WARNING: error reading CDX page", p, ":", err)
		}
		resp.Body.Close()
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
						// Robustly split the raw query into pairs on '&' and ';' so
						// URLs that the parser may not split correctly still yield
						// individual parameter keys.
						pairs := strings.FieldsFunc(u.RawQuery, func(r rune) bool {
							return r == '&' || r == ';'
						})
						for _, p := range pairs {
							if p == "" {
								continue
							}
							k := p
							if idx := strings.Index(p, "="); idx >= 0 {
								k = p[:idx]
							}
							if k == "" {
								continue
							}
							if un, err := url.QueryUnescape(k); err == nil {
								k = un
							}
							results <- k // each key on its own line
						}
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
