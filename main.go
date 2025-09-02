package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp" // External package to control Chrome/Chromium browser
)

// It checks if the file exists
// If the file exists, it returns true
// If the file does not exist, it returns false
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// Remove a file from the file system
func removeFile(path string) {
	err := os.Remove(path)
	if err != nil {
		log.Println(err)
	}
}

// Checks whether a given directory exists
func directoryExists(path string) bool {
	directory, err := os.Stat(path) // Get info for the path
	if err != nil {
		return false // Return false if error occurs
	}
	return directory.IsDir() // Return true if it's a directory
}

// Creates a directory at given path with provided permissions
func createDirectory(path string, permission os.FileMode) {
	err := os.Mkdir(path, permission) // Attempt to create directory
	if err != nil {
		log.Println(err) // Log error if creation fails
	}
}

// Verifies whether a string is a valid URL format
func isUrlValid(uri string) bool {
	_, err := url.ParseRequestURI(uri) // Try parsing the URL
	return err == nil                  // Return true if valid
}

// Extracts filename from full path (e.g. "/dir/file.pdf" → "file.pdf")
func getFilename(path string) string {
	return filepath.Base(path) // Use Base function to get file name only
}

// Removes all instances of a specific substring from input string
func removeSubstring(input string, toRemove string) string {
	result := strings.ReplaceAll(input, toRemove, "") // Replace substring with empty string
	return result
}

// Gets the file extension from a given file path
func getFileExtension(path string) string {
	return filepath.Ext(path) // Extract and return file extension
}

// Converts a raw URL into a sanitized PDF filename safe for filesystem
func urlToFilename(rawURL string) string {
	lower := strings.ToLower(rawURL) // Convert URL to lowercase
	lower = getFilename(lower)       // Extract filename from URL

	reNonAlnum := regexp.MustCompile(`[^a-z0-9]`)   // Regex to match non-alphanumeric characters
	safe := reNonAlnum.ReplaceAllString(lower, "_") // Replace non-alphanumeric with underscores

	safe = regexp.MustCompile(`_+`).ReplaceAllString(safe, "_") // Collapse multiple underscores into one
	safe = strings.Trim(safe, "_")                              // Trim leading and trailing underscores

	var invalidSubstrings = []string{
		"_pdf", // Substring to remove from filename
	}

	for _, invalidPre := range invalidSubstrings { // Remove unwanted substrings
		safe = removeSubstring(safe, invalidPre)
	}

	if getFileExtension(safe) != ".pdf" { // Ensure file ends with .pdf
		safe = safe + ".pdf"
	}

	return safe // Return sanitized filename
}

// Downloads a PDF from given URL and saves it in the specified directory
func downloadPDF(finalURL, outputDir string) bool {
	filename := strings.ToLower(urlToFilename(finalURL)) // Sanitize the filename
	filePath := filepath.Join(outputDir, filename)       // Construct full path for output file

	if fileExists(filePath) { // Skip if file already exists
		log.Printf("File already exists, skipping: %s", filePath)
		return false
	}

	client := &http.Client{Timeout: 15 * time.Minute} // Create HTTP client with timeout

	// Create a new request so we can set headers
	req, err := http.NewRequest("GET", finalURL, nil)
	if err != nil {
		log.Printf("Failed to create request for %s: %v", finalURL, err)
		return false
	}

	// Set a User-Agent header
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to download %s: %v", finalURL, err)
		return false
	}
	defer resp.Body.Close() // Ensure response body is closed

	if resp.StatusCode != http.StatusOK { // Check if response is 200 OK
		log.Printf("Download failed for %s: %s", finalURL, resp.Status)
		return false
	}

	contentType := resp.Header.Get("Content-Type") // Get content type of response
	if !strings.Contains(contentType, "binary/octet-stream") &&
		!strings.Contains(contentType, "application/pdf") {
		log.Printf("Invalid content type for %s: %s (expected PDF)", finalURL, contentType)
		return false
	}

	var buf bytes.Buffer                     // Create a buffer to hold response data
	written, err := io.Copy(&buf, resp.Body) // Copy data into buffer
	if err != nil {
		log.Printf("Failed to read PDF data from %s: %v", finalURL, err)
		return false
	}
	if written == 0 { // Skip empty files
		log.Printf("Downloaded 0 bytes for %s; not creating file", finalURL)
		return false
	}

	out, err := os.Create(filePath) // Create output file
	if err != nil {
		log.Printf("Failed to create file for %s: %v", finalURL, err)
		return false
	}
	defer out.Close() // Ensure file is closed after writing

	if _, err := buf.WriteTo(out); err != nil { // Write buffer contents to file
		log.Printf("Failed to write PDF to file for %s: %v", finalURL, err)
		return false
	}

	log.Printf("Successfully downloaded %d bytes: %s → %s", written, finalURL, filePath) // Log success
	return true
}

// extractBaseDomain takes a URL string and returns only the bare domain name
// without any subdomains or suffixes (e.g., ".com", ".org", ".co.uk").
func extractBaseDomain(inputUrl string) string {
	// Parse the input string into a structured URL object
	parsedUrl, parseError := url.Parse(inputUrl)

	// If parsing fails, log the error and return an empty string
	if parseError != nil {
		log.Println("Error parsing URL:", parseError)
		return ""
	}

	// Extract the hostname (e.g., "sub.example.com")
	hostName := parsedUrl.Hostname()

	// Split the hostname into parts separated by "."
	// For example: "sub.example.com" -> ["sub", "example", "com"]
	parts := strings.Split(hostName, ".")

	// If there are at least 2 parts, the second last part is usually the domain
	// Example: "sub.example.com" -> "example"
	//          "blog.my-site.co.uk" -> "my-site"
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}

	// If splitting fails or domain structure is unusual, return the hostname itself
	return hostName
}

// getFinalURL navigates to a given URL using headless Chrome
// and follows all redirects (HTTP, meta refresh, JS) until the URL stabilizes.
func getFinalURL(inputURL string) string {
	// Configure Chrome options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true), // Run headless
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
	)

	// Create allocator context
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	// Context with timeout
	ctx, cancel := context.WithTimeout(allocCtx, 2*time.Minute)
	defer cancel()

	// New browser tab context
	ctx, cancelCtx := chromedp.NewContext(ctx)
	defer cancelCtx()

	var currentURL, lastURL string
	start := time.Now()

	for {
		// Navigate and capture URL
		err := chromedp.Run(ctx,
			chromedp.Navigate(inputURL),
			chromedp.WaitReady("body", chromedp.ByQuery),
			chromedp.Sleep(3*time.Second), // let JS/meta redirects fire
			chromedp.Location(&currentURL),
		)
		if err != nil {
			log.Printf("chromedp error: %v", err)
			return ""
		}

		// Stop if URL has stabilized
		if currentURL == lastURL {
			return currentURL
		}

		// Prepare for next loop
		lastURL = currentURL
		inputURL = currentURL

		// Safety cutoff
		if time.Since(start) > (3 * time.Minute) {
			log.Printf("redirect loop timeout at: %s", currentURL)
			return currentURL
		}
	}
}

func main() {
	outputDir := "PDFs/" // Directory to store downloaded PDFs

	if !directoryExists(outputDir) { // Check if directory exists
		createDirectory(outputDir, 0o755) // Create directory with read-write-execute permissions
	}

	// The remote domain name.
	remoteDomainName := "https://beaumontproductsingredients.com"

	// The location to the local.
	localFile := extractBaseDomain(remoteDomainName) + ".html"
	// Check if the local file exists.
	if fileExists(localFile) {
		removeFile(localFile)
	}
	// The location to the remote url.
	remoteURL := []string{
		"http://www.docs.citgo.com/msds_pi/C10005B.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622613001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622613001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622615001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622615001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622625001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622625001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10005A.pdf",
		"http://www.docs.citgo.com/msds_pi/622610001.pdf",
		"http://www.docs.citgo.com/msds_pi/622610001-s.pdf",
		"http://www.docs.citgo.com/msds_pi/622630001.pdf",
		"http://www.docs.citgo.com/msds_pi/622630001-s.pdf",
		"http://www.docs.citgo.com/msds_pi/622640001.pdf",
		"http://www.docs.citgo.com/msds_pi/622640001-s.pdf",
		"http://www.docs.citgo.com/msds_pi/622650001.pdf",
		"http://www.docs.citgo.com/msds_pi/622650001-s.pdf",
		"http://www.docs.citgo.com/msds_pi/C10246.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622721001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622721001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622723001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622723001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10128.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632020001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632020001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632019001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632019001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10234.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622676001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622676001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622677001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622677001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10147.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632495001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632495001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632496001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632496001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632497001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632497001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632498001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632498001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10139.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633746001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633746001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10139.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633748001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633748001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10099.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637141001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637141001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10146.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632566001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632566001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632568001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632568001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10093.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632542001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632542001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632348001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632348001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632595001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632595001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C12224.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632537001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632537001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10094.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632531001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632531001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632532001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632532001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632533001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632533001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632534001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632534001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632535001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632535001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10161.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632482001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632482001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10096.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632554001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632554001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632555001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632555001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632557001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632557001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10043.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632558001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632558001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10045.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=635013001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=635013001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=635015001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=635015001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=635017001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=635017001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=635023001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=635023001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10101.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637142001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637142001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10238.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633626001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633626001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633621001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633621001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633622001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633622001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10030.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633623001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633623001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10203.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633606001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633606001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633607001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633607001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633608001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633608001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633609001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633609001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633610001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633610001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633611001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633611001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10232.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633618001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633618001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10149.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633602001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633602001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633603001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633603001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10210.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633625001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633625001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10083.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637130001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637130001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10069.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632210001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632210001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632032001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632032001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632057001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632057001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10235.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632056001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632056001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10197.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632090001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632090001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632085001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632085001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632091001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632091001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10071.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632047001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632047001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632045001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632045001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632046001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632046001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10066.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632033001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632033001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632034001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632034001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10274.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632042001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632042001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632043001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632043001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632040001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632040001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10182.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632051001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632051001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10092.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632515001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632515001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10242.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633313001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633313001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10012.pdf",
		"https://www.docs.citgo.com/msds_pi/631310001.pdf",
		"https://www.docs.citgo.com/msds_pi/631310001-s.pdf",
		"https://www.docs.citgo.com/msds_pi/631320001.pdf",
		"https://www.docs.citgo.com/msds_pi/631320001-s.pdf",
		"http://www.docs.citgo.com/msds_pi/C10148.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632493001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632493001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10011.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631502001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631502001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10223.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620883001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620883001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620884001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620884001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10002.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620802001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620802001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620805001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620805001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620813001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620813001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620814001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620814001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620825001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620825001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10002.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620903001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620903001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620904001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620904001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10200.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620854001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620854001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10144.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620858001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620858001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620860001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620860001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620859001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620859001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620861001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620861001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620863001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620863001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10124.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620891001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620891001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620892001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620892001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620893001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620893001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620894001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620894001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620898001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620898001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620899001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620899001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620900001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=620900001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10015.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631809001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631809001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631814001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631814001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10019.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633105001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633105001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10016.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633122001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633122001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10160.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633179001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633179001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C12230.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=C12230_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=C12230_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10165.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633140001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633140001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10020.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633321001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633321001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633323001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633323001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633325001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633325001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633326001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633326001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10162.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633135001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633135001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10168.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633131001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633131001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10229.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633137001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633137001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10022.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633310001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633310001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633311001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633311001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10208.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637163001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637163001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637167001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637167001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637169001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637169001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637170001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637170001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637172001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637172001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/637174001.pdf",
		"http://www.docs.citgo.com/msds_pi/637174001-s.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637175001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637175001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637176001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637176001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10207.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633592001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633592001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10037.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633935001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633935001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10029.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633491001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633491001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633492001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633492001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633493001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633493001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10227.pdf",
		"http://www.docs.citgo.com/msds_pi/638110001.pdf",
		"http://www.docs.citgo.com/msds_pi/638110001-s.pdf",
		"http://www.docs.citgo.com/msds_pi/C10213.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631056001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631056001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10049.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632581001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632581001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632582001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632582001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632583001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632583001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632584001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632584001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632585001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632585001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632587001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632587001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632588001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632588001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10048.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632571001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632571001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632572001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632572001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632573001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632573001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632574001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632574001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632575001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632575001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632577001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632577001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632579001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632579001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10163.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632543001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632543001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632544001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632544001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632547001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632547001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632548001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632548001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632549001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632549001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10095.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632523001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632523001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632525001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632525001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632526001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632526001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632527001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632527001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632528001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=632528001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10098.pdf",
		"http://www.docs.citgo.com/msds_pi/632580001.pdf",
		"http://www.docs.citgo.com/msds_pi/632580001-s.pdf",
		"http://www.docs.citgo.com/msds_pi/632591001.pdf",
		"http://www.docs.citgo.com/msds_pi/632591001-s.pdf",
		"http://www.docs.citgo.com/msds_pi/C10097.pdf",
		"http://www.docs.citgo.com/msds_pi/C10079.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=643205001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=643205001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10127.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633091001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633091001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633092001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633092001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10047.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631110001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631110001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631120001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631120001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631130001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631130001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631140001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631140001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631150001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631150001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631170001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631170001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631180001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631180001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631190001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631190001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10196.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633329001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633329001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10206.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=648326001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=648326001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10032.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=648346001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=648346001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10082.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=661290001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=661290001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10262.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633613001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633613001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633614001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633614001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C12225.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633495001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633495001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10137.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633615001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633615001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633616001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633616001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633617001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633617001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10122.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637131001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637131001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10252.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=638155001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=638155001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=638156001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=638156001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10085.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=643102001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=643102001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=643105001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=643105001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=643108001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=643108001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10086.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=634104001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=634104001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=634105001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=634105001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10034.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633013001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633013001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633001001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633001001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633002001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633002001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633003001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633003001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633006001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633006001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633008001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633008001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633009001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633009001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633010001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633010001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633012001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633012001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633014001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633014001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10053.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633044001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633044001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633045001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633045001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633047001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633047001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633050001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633050001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10035.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633715001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633715001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633720001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633720001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633730001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633730001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633745001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633745001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10259.pdf",
		"https://www.docs.citgo.com/msds_pi/C10041.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633792001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633792001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10087.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633821001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633821001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10125.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=627910001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=627910001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=627915001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=627915001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=627935001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=627935001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=627950001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=627950001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10089.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633212001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633212001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633214001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633214001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633217001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633217001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10036.pdf",
		"http://www.docs.citgo.com/msds_pi/637001001.pdf",
		"http://www.docs.citgo.com/msds_pi/637001001-s.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637019001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637019001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637004001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637004001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637006001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637006001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637099001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637099001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637015001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637015001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10192.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622685001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622685001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10073.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=649269001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=649269001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=649069001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=649069001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10247.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622722001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622722001_MX_ES",
		"https://www.docs.citgo.com/msds_pi/C10273.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622901001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622901001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10058.pdf",
		"http://www.docs.citgo.com/msds_pi/C10090.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637203001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637203001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637210001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637210001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637220001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637220001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637320001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=637320001_MX_ES",
		"https://www.docs.citgo.com/msds_pi/C10269.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622686001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=622686001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10250.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631817001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=631817001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10254.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655566001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655566001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655565001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655565001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10249.pdf",
		"https://www.docs.citgo.com/msds_pi/C10260.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655360001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655360001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655358001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655358001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10135.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655780001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655780001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10154.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655351001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655351001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655352001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655352001_MX_ES",
		"https://www.docs.citgo.com/msds_pi/C10270.pdf",
		"http://www.docs.citgo.com/msds_pi/C10154.pdf",
		"http://www.docs.citgo.com/msds_pi/C10025.pdf",
		"http://www.docs.citgo.com/msds_pi/C10239.pdf",
		"http://www.docs.citgo.com/msds_pi/C10191.pdf",
		"http://www.docs.citgo.com/msds_pi/C10198.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655387001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655387001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10193.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655343001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655343001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655344001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655344001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10194.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655366001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655366001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655367001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655367001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10248.pdf",
		"http://www.docs.citgo.com/msds_pi/C10076.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=649110001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=649110001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10152.pdf",
		"http://www.docs.citgo.com/msds_pi/C10244.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=649281001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=649281001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10179.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655424001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655424001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655427001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655427001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10214.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655542001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=655542001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10142.pdf",
		"https://www.docs.citgo.com/msds_pi/C10266.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=638120001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=638120001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=638123001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=638123001_MX_ES",
		"https://www.docs.citgo.com/msds_pi/C10231.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633222001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633222001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633223001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633223001_MX_ES",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633224001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633224001_MX_ES",
		"https://www.docs.citgo.com/msds_pi/C10263.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=669490001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=669490001_MX_ES",
		"http://www.docs.citgo.com/msds_pi/C10257.pdf",
		"http://www.docs.citgo.com/msds_pi/C10023.pdf",
		"https://www.docs.citgo.com/msds_pi/C10265.pdf",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633794001_US_EN",
		"https://apps.spheracloud.net/LoginFetch.aspx?userid=7EEpJ1QmzKUA&companyid=37NzIg6inj0A&method=FETCHSDS&searchfield=SN&searchvalue=633794001_MX_ES",
	}
	// Loop through all extracted PDF URLs
	for _, urls := range remoteURL {
		// Get final resolved URL (in case of redirects)
		resolvedPDFURL := getFinalURL(urls)
		if isUrlValid(resolvedPDFURL) { // Check if the final URL is valid
			downloadPDF(resolvedPDFURL, outputDir) // Download the PDF
		}
	}
}
