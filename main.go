package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

const (
	pageLength uint32 = 24
	rootUrl    string = "https://www.rightmove.co.uk"
)

var (
	propertyResultCount uint32
	pageCount           uint32
	progressBar         *ProgressBar
	userAgentList       = []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/93.0.4577.82 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Safari/537.36 Edg/87.0.664.75",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.102 Safari/537.36 Edge/18.18363",
	}

	arguments struct {
		zipCodeUrl string
		postCodeId string
	}

	config struct {
		ZipCode     string   `yaml:"zipCode"`
		Radius      float32  `yaml:"radius"`
		MinBedrooms int      `yaml:"minBedrooms"`
		Keywords    []string `yaml:"keywords"`
		Tenure      []string `yaml:"tenure"`
	}
)

type Property struct {
	Url          string
	Street       string
	PropertyType string
	Beds         int
	Baths        int
	Size         int
	Tenure       string
	Price        int
}

// Search "Self contained flat" in the description

// validateTenure checks if all tenure values are valid and returns an error if not
func validateTenure(tenures []string) error {
	validTenures := map[string]bool{
		"FREEHOLD":          true,
		"LEASEHOLD":         true,
		"SHARE_OF_FREEHOLD": true,
	}

	for _, tenure := range tenures {
		if !validTenures[strings.ToUpper(tenure)] {
			return fmt.Errorf("invalid tenure value: %s. Valid values are: FREEHOLD, LEASEHOLD, SHARE_OF_FREEHOLD", tenure)
		}
	}
	return nil
}

// genSearchUrl generates the Rightmove API search URL.
func genUrl(index uint32) string {
	baseUrl := rootUrl +
		"/api/property-search/listing/search" +
		fmt.Sprintf("?searchLocation=%s", arguments.zipCodeUrl) +
		"&useLocationIdentifier=true" +
		fmt.Sprintf("&locationIdentifier=POSTCODE%%5E%s", arguments.postCodeId) +
		fmt.Sprintf("&radius=%.1f", config.Radius) +
		fmt.Sprintf("&minBedrooms=%d", config.MinBedrooms) +
		"&_includeSSTC=on" +
		"&includeSSTC=true" +
		fmt.Sprintf("&index=%d", index) +
		"&sortType=2" +
		"&channel=BUY" +
		"&transactionType=BUY" +
		"&displayLocationIdentifier=undefined"

	// Add tenure types if specified
	if len(config.Tenure) > 0 {
		// Convert tenure values to uppercase and URL encode them
		var tenureValues []string
		for _, tenure := range config.Tenure {
			tenureValues = append(tenureValues, strings.ToUpper(tenure))
		}
		tenureParam := url.QueryEscape(strings.Join(tenureValues, ","))
		baseUrl += fmt.Sprintf("&tenureTypes=%s", tenureParam)
	}

	return baseUrl
}

func getPropertyCount() {
	req, err := http.NewRequest("GET", genUrl(0), nil)
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		return
	}
	req.Header.Set("User-Agent", userAgentList[rand.Intn(len(userAgentList))])
	req.Header.Set("Accept", "application/json, text/plain, */*")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to fetch property count: %v", err)
		return
	}
	defer resp.Body.Close()

	var payload struct {
		ResultCount string `json:"resultCount"`
		Pagination  struct {
			Total   int `json:"total"`
			Options []struct {
				Value string `json:"value"`
			} `json:"options"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Printf("Could not parse search JSON: %v", err)
		return
	}

	if payload.ResultCount != "" {
		if v, err := strconv.Atoi(payload.ResultCount); err == nil {
			propertyResultCount = uint32(v)
		}
	}

	if len(payload.Pagination.Options) > 0 {
		pageCount = uint32(len(payload.Pagination.Options))
	} else if payload.Pagination.Total > 0 {
		pageCount = (propertyResultCount + pageLength - 1) / pageLength
	} else if propertyResultCount > 0 {
		pageCount = (propertyResultCount + pageLength - 1) / pageLength
	}

	log.Printf("Found %d total results across %d pages", propertyResultCount, pageCount)
}

func setMetadata() {
	arguments.zipCodeUrl = strings.ReplaceAll(config.ZipCode, " ", "+")
	var postCodeContainer struct {
		Matches []struct{ Id string } `json:"matches"`
	}
	r, err := http.Get("https://los.rightmove.co.uk/typeahead?query=" + arguments.zipCodeUrl)
	if err != nil {
		log.Fatalln("Could not GET los.rightmove.co.uk:", err)
	}
	if err := json.NewDecoder(r.Body).Decode(&postCodeContainer); err != nil {
		log.Fatalln("Could not decode response: ", err)
	}
	r.Body.Close()
	arguments.postCodeId = postCodeContainer.Matches[0].Id
}

func main() {
	// Initialize logging to file (reset on each execution)
	logFile, err := os.Create("scraper.log")
	if err != nil {
		// If we cannot create the log file, fall back to default output
		fmt.Fprintf(os.Stderr, "Could not create scraper.log: %v\n", err)
	} else {
		log.SetOutput(logFile)
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		defer logFile.Close()
	}

	configFile, err := os.ReadFile("config.yaml")
	if err != nil {
		log.Fatalf("Could not read config.yaml: %v\n", err)
	}
	if err := yaml.Unmarshal(configFile, &config); err != nil {
		log.Fatalf("Could not parse config.yaml: %v\n", err)
	}

	// Validate tenure values if provided
	if len(config.Tenure) > 0 {
		if err := validateTenure(config.Tenure); err != nil {
			log.Fatalf("Configuration error: %v\n", err)
		}
	}

	// Print a concise bullet list of the search conditions to stdout
	fmt.Println()
	fmt.Println("Search:")
	fmt.Printf("- zip: %s\n", config.ZipCode)
	fmt.Printf("- radius: %.1fmi\n", config.Radius)
	fmt.Printf("- minBeds: %d\n", config.MinBedrooms)
	fmt.Printf("- keywords: %s\n", strings.Join(config.Keywords, ", "))
	if len(config.Tenure) > 0 {
		fmt.Printf("- tenure: %s\n", strings.Join(config.Tenure, ", "))
	}
	fmt.Println()

	setMetadata()
	getPropertyCount()

	// Announce start before showing the progress bar, including results count
	fmt.Printf("Found %d results: Scrapping RightMove Content...\n", propertyResultCount)
	fmt.Println()

	// Initialize progress bar with total equal to number of properties
	progressBar = NewProgressBar(propertyResultCount)
	progressBar.Draw()

	var properties []Property
	for i := uint32(0); i < pageCount; i++ {
		pageIndex := i * pageLength
		pageProps, err := fetchPropertiesPage(pageIndex)
		if err != nil {
			log.Printf("Failed to fetch page at index %d: %v", pageIndex, err)
			continue
		}
		properties = append(properties, pageProps...)
	}

	// Write to CSV after scraping completes
	csvPath := "results.csv"
	if err := writeCSV(csvPath, properties); err != nil {
		log.Fatalf("Failed to write CSV: %v", err)
	}

	log.Println("Scraping completed successfully! Results saved to results.csv")

	// Inform user via stdout where the CSV is located
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("\nResults CSV: %s\n", csvPath)
	} else {
		fmt.Printf("\nResults CSV: %s\n", filepath.Join(cwd, csvPath))
	}
}

// fetchPropertiesPage fetches a search page from the API, updates the progress bar for
// each property in the response, applies keyword filtering (if any), and returns
// the matched properties mapped to our Property struct.
func fetchPropertiesPage(index uint32) ([]Property, error) {
	url := genUrl(index)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// Provide reasonable headers to mimic a browser
	req.Header.Set("User-Agent", userAgentList[rand.Intn(len(userAgentList))])
	req.Header.Set("Accept", "application/json, text/plain, */*")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		Properties []struct {
			DisplayAddress              string `json:"displayAddress"`
			PropertyUrl                 string `json:"propertyUrl"`
			Bedrooms                    int    `json:"bedrooms"`
			Bathrooms                   int    `json:"bathrooms"`
			PropertyTypeFullDescription string `json:"propertyTypeFullDescription"`
			DisplaySize                 string `json:"displaySize"`
			Summary                     string `json:"summary"`
			Price                       struct {
				Amount int `json:"amount"`
			} `json:"price"`
			Tenure struct {
				TenureType string `json:"tenureType"`
			} `json:"tenure"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	var matched []Property
	for _, pr := range payload.Properties {
		if progressBar != nil {
			progressBar.Increment()
		}
		// Apply keyword filter (case-insensitive) over summary text
		if len(config.Keywords) > 0 {
			if !containsAny(strings.ToLower(pr.Summary), config.Keywords) {
				continue
			}
		}
		var p Property
		p.Url = rootUrl + pr.PropertyUrl
		p.Street = sanitizeAddress(pr.DisplayAddress)
		p.PropertyType = pr.PropertyTypeFullDescription
		p.Beds = pr.Bedrooms
		p.Baths = pr.Bathrooms
		// Parse size best-effort from displaySize
		if pr.DisplaySize != "" {
			var digits strings.Builder
			for _, r := range pr.DisplaySize {
				if r >= '0' && r <= '9' {
					digits.WriteRune(r)
				}
			}
			if digits.Len() > 0 {
				if v, err := strconv.Atoi(digits.String()); err == nil {
					p.Size = v
				}
			}
		}
		p.Tenure = pr.Tenure.TenureType
		p.Price = pr.Price.Amount
		matched = append(matched, p)
	}

	return matched, nil
}

// containsAny returns true if haystack contains any of the keywords (case-insensitive).
func containsAny(haystack string, keywords []string) bool {
	hat := strings.ToLower(haystack)
	for _, kw := range keywords {
		if strings.Contains(hat, strings.ToLower(strings.TrimSpace(kw))) {
			return true
		}
	}
	return false
}

func writeCSV(path string, properties []Property) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()

	if err := w.Write([]string{"url", "street", "propertyType", "beds", "baths", "size", "tenure", "price"}); err != nil {
		return err
	}

	for _, p := range properties {
		record := []string{
			p.Url,
			p.Street,
			p.PropertyType,
			strconv.Itoa(p.Beds),
			strconv.Itoa(p.Baths),
			strconv.Itoa(p.Size),
			p.Tenure,
			strconv.Itoa(p.Price),
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}

	w.Flush()
	return w.Error()
}

// sanitizeAddress removes newlines and collapses excess whitespace in addresses.
func sanitizeAddress(text string) string {
	if text == "" {
		return text
	}
	// Replace line breaks and tabs with spaces
	t := strings.ReplaceAll(text, "\r\n", " ")
	t = strings.ReplaceAll(t, "\n", " ")
	t = strings.ReplaceAll(t, "\r", " ")
	t = strings.ReplaceAll(t, "\t", " ")
	// Collapse multiple spaces and trim
	parts := strings.Fields(t)
	return strings.Join(parts, " ")
}

// isUnknownValue returns true for placeholders used by the site like
// "Ask agent" or "Ask developer".
func isUnknownValue(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	return t == "" || t == "ask agent" || t == "ask developer"
}
