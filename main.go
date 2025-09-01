package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/goccy/go-yaml"
	"github.com/gocolly/colly"
)

const (
	pageLength uint32 = 24
	rootUrl    string = "https://www.rightmove.co.uk"
	askAgent   string = "Ask agent"
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

// ProgressBar implementation moved to progress_bar.go

func genUrl(index uint32) string {
	return rootUrl +
		"/property-for-sale/" +
		"find.html" +
		fmt.Sprintf("?searchLocation=%s", arguments.zipCodeUrl) +
		"&useLocationIdentifier=true" +
		fmt.Sprintf("&locationIdentifier=POSTCODE%%5E%s", arguments.postCodeId) +
		"&buy=For+sale" +
		fmt.Sprintf("&radius=%.1f", config.Radius) +
		fmt.Sprintf("&minBedrooms=%d", config.MinBedrooms) +
		"&propertyTypes=flat%2Cdetached%2Csemi-detached%2Cterraced%2Cbungalow%2Cland%2Cpark-home" +
		"&_includeSSTC=on" +
		"&includeSSTC=true" +
		fmt.Sprintf("&index=%d", index) +
		"&sortType=2" +
		"&channel=BUY" +
		"&transactionType=BUY" +
		"&displayLocationIdentifier=undefined" +
		"&tenureTypes=FREEHOLD"

}

func createCollector() *colly.Collector {
	c := colly.NewCollector()

	// Enable user agent rotation to appear more like real browsers
	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", userAgentList[rand.Intn(len(userAgentList))])
		// Add a random delay between requests (1-3 seconds)
		time.Sleep(time.Duration(1000+rand.Intn(2000)) * time.Millisecond)
	})

	// Better error handling - don't panic on rate limits
	c.OnError(func(resp *colly.Response, err error) {
		if resp != nil && resp.StatusCode == 429 {
			log.Println("Rate limited (429). Waiting 10 seconds before continuing...")
			time.Sleep(10 * time.Second)
		} else {
			log.Println("Request failed:", err)
		}
	})

	// Optional: Log successful responses
	c.OnResponse(func(r *colly.Response) {
		log.Printf("Visited: %s (Status: %d)\n", r.Request.URL, r.StatusCode)
	})

	return c
}

func getPropertyCount() {
	c := createCollector()

	c.OnHTML(".ResultsCount_resultsCount__Kqeah", func(e *colly.HTMLElement) {
		e.DOM.Find("span").Each(func(i int, s *goquery.Selection) {
			resultCount, err := strconv.Atoi(s.Text())
			if err != nil {
				log.Fatalln("Could not strconv.Atoi property result count: ", err)
			}
			propertyResultCount = uint32(resultCount)
		})
	})

	c.OnHTML(".Pagination_pageSelectContainer__zt0rg", func(e *colly.HTMLElement) {
		e.DOM.Find("span").Each(func(i int, s *goquery.Selection) {
			text := s.Text()
			if text == "Page " {
				return
			}
			// Handle format like "of 16" with non-breaking space (U+00A0)
			if after, ok := strings.CutPrefix(text, "of\u00a0"); ok {
				pageCountText := after
				pageCountValue, err := strconv.Atoi(pageCountText)
				if err != nil {
					log.Printf("Could not parse page count from 'of %s', error: %v", pageCountText, err)
					return
				}
				pageCount = uint32(pageCountValue)
				log.Printf("Found %d total results across %d pages", propertyResultCount, pageCount)
				return
			}
			log.Printf("Unhandled pagination text: '%s'", text)
		})
	})

	c.Visit(genUrl(0))
}

func findProperties(startingIndex uint32, propertyUrls chan string, wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	c := createCollector()
	index := startingIndex

	c.OnHTML("#l-searchResults", func(e *colly.HTMLElement) {
		e.DOM.Find("[class=propertyCard-link]").Each(func(i int, s *goquery.Selection) {
			propertyUrl, _ := s.Attr("href")
			propertyUrls <- propertyUrl
			index++
		})
	})

	c.OnScraped(func(r *colly.Response) {
		// log.Printf("Processed page %d/%d (%d properties). Moving on...\n", index/pageLength, pageCount, pageLength)
	})

	c.Visit(genUrl(index))
}

func filterProperties(filteredProperties chan string, propertyUrls <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()
	var visitWg sync.WaitGroup

	c := createCollector()
	// Ensure we decrement visitWg on failures too
	c.OnError(func(r *colly.Response, err error) {
		if progressBar != nil {
			progressBar.Increment()
		}
		visitWg.Done()
	})
	c.OnHTML(".STw8udCxUaBUMfOOZu0iL._3nPVwR0HZYQah5tkVJHFh5", func(e *colly.HTMLElement) {
		for _, kw := range config.Keywords {
			if strings.Contains(e.Text, kw) {
				filteredProperties <- e.Request.URL.String()
				break
			}
		}
	})

	c.OnResponse(func(r *colly.Response) {
		if progressBar != nil {
			progressBar.Increment()
		}
		visitWg.Done()
	})

	for propertyUrl := range propertyUrls {
		fullPropertyUrl := rootUrl + propertyUrl
		visitWg.Add(1)
		if err := c.Visit(fullPropertyUrl); err != nil {
			// e.g., ErrAlreadyVisited – callbacks won't fire, so decrement here
			if progressBar != nil {
				progressBar.Increment()
			}
			visitWg.Done()
		}
	}

	visitWg.Wait()
	log.Printf("Filter worker finished processing all property URLs")
}

func getMetadataFromProperties(propertyMetadata chan Property, filteredProperties <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()
	var getDataWg sync.WaitGroup

	c := createCollector()
	// Ensure we decrement getDataWg on failures too
	c.OnError(func(r *colly.Response, err error) {
		getDataWg.Done()
	})
	c.OnHTML(".WJG_W7faYk84nW-6sCBVi", func(e *colly.HTMLElement) {
		var p Property
		p.Url = e.Request.URL.String()

		p.Street = e.DOM.Find("[itemprop=streetAddress]").First().Text()

		e.DOM.Find("._1hV1kqpVceE9m-QrX_hWDN").Each(func(i int, s *goquery.Selection) {
			switch i {
			case 0:
				p.PropertyType = s.Text()
			case 1:
				bedsText := s.Text()
				if bedsText == askAgent {
					// Expected case where bedrooms info is not available
					p.Beds = 0
				} else {
					beds, err := strconv.Atoi(bedsText)
					if err != nil {
						log.Printf("Could not parse bedrooms - received value: '%s', error: %v, setting to 0", bedsText, err)
						p.Beds = 0
					} else {
						p.Beds = beds
					}
				}
			case 2:
				bathsText := s.Text()
				if bathsText == askAgent {
					// Expected case where bathrooms info is not available
					p.Baths = 0
				} else {
					baths, err := strconv.Atoi(bathsText)
					if err != nil {
						log.Printf("Could not parse bathrooms - received value: '%s', error: %v, setting to 0", bathsText, err)
						p.Baths = 0
					} else {
						p.Baths = baths
					}
				}
			case 3:
				sizeString := s.Parent().Children().Last().Text()
				if strings.Contains(sizeString, " sq m") {
					sizeText := strings.Split(sizeString, " sq m")[0]
					size, err := strconv.Atoi(sizeText)
					if err != nil {
						log.Printf("Could not parse surface area - raw value: '%s', extracted: '%s', error: %v, setting to 0", sizeString, sizeText, err)
						p.Size = 0
					} else {
						p.Size = size
					}
				} else if sizeString == askAgent {
					// Expected cases where size is not available
					p.Size = 0
				} else {
					log.Printf("Surface area in unexpected format - received value: '%s', setting to 0", sizeString)
					p.Size = 0
				}
			case 4:
				p.Tenure = s.Text()
			}
		})

		// Price extraction with robust fallbacks
		priceText := strings.TrimSpace(e.DOM.Find("div._1gfnqJ3Vtd1z40MlC0MzXu").Find("span").First().Text())
		if priceText == "" {
			priceText = strings.TrimSpace(e.DOM.Find("div._1gfnqJ3Vtd1z40MlC0MzXu").First().Text())
		}
		if priceText == "" {
			priceText = strings.TrimSpace(e.DOM.Find("article._2fFy6nQs_hX4a6WEDR-B-6 div._1gfnqJ3Vtd1z40MlC0MzXu").First().Text())
		}
		p.Price = parsePrice(priceText)

		propertyMetadata <- p
	})

	c.OnResponse(func(r *colly.Response) { getDataWg.Done() })

	for propertyUrl := range filteredProperties {
		getDataWg.Add(1)
		if err := c.Visit(propertyUrl); err != nil {
			// e.g., ErrAlreadyVisited – callbacks won't fire, so decrement here
			getDataWg.Done()
		}
	}

	getDataWg.Wait()
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

	// Print a concise bullet list of the search conditions to stdout
	fmt.Println()
	fmt.Println("Search:")
	fmt.Printf("- zip: %s\n", config.ZipCode)
	fmt.Printf("- radius: %.1fmi\n", config.Radius)
	fmt.Printf("- minBeds: %d\n", config.MinBedrooms)
	fmt.Printf("- keywords: %s\n", strings.Join(config.Keywords, ", "))
	fmt.Println()

	setMetadata()
	getPropertyCount()

	// Announce start before showing the progress bar, including results count
	fmt.Printf("Found %d results: Scrapping RightMove Content...\n", propertyResultCount)
	fmt.Println()

	// Initialize progress bar with total equal to number of properties
	progressBar = NewProgressBar(propertyResultCount)
	progressBar.Draw()

	propertyUrls := make(chan string, int(pageCount))
	filteredProperties := make(chan string, int(propertyResultCount))
	propertyMetadata := make(chan Property, int(propertyResultCount))
	metaDone := make(chan struct{})

	// Limit concurrent workers to reduce load on Rightmove
	maxWorkers := intMin(3, int(propertyResultCount))

	var filterWg sync.WaitGroup
	var metaWg sync.WaitGroup

	// Limited concurrent filter workers
	for i := 0; i < maxWorkers; i++ {
		filterWg.Add(1)
		go filterProperties(filteredProperties, propertyUrls, &filterWg)
	}

	// Limited concurrent metadata workers
	for i := 0; i < maxWorkers; i++ {
		metaWg.Add(1)
		go getMetadataFromProperties(propertyMetadata, filteredProperties, &metaWg)
	}

	// Process pages sequentially to avoid overwhelming the server
	go func() {
		for i := uint32(0); i < pageCount; i++ {
			findProperties(i*pageLength, propertyUrls, nil)
			// Add delay between pages
			time.Sleep(2 * time.Second)
		}
		close(propertyUrls)
		log.Println("Finished processing all pages, closed propertyUrls channel")
	}()

	// Close filteredProperties after all filter workers finish
	go func() {
		filterWg.Wait()
		close(filteredProperties)
		log.Println("All filter workers finished, closed filteredProperties channel")
	}()

	// Close propertyMetadata after all metadata workers finish
	go func() {
		metaWg.Wait()
		close(propertyMetadata)
		log.Println("All metadata workers finished, closed propertyMetadata channel")
		close(metaDone)
	}()

	// Collect metadata entries into memory
	var properties []Property
	for p := range propertyMetadata {
		properties = append(properties, p)
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

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parsePrice converts a currency string like "£325,000" or "Guide Price £325,000" into an integer 325000.
func parsePrice(text string) int {
	text = strings.TrimSpace(text)
	if text == "" || text == askAgent {
		return 0
	}

	var digits strings.Builder
	for _, r := range text {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}

	if digits.Len() == 0 {
		return 0
	}

	valueString := digits.String()
	value, err := strconv.Atoi(valueString)
	if err != nil {
		log.Printf("Could not parse price - raw value: '%s', extracted: '%s', error: %v, setting to 0", text, valueString, err)
		return 0
	}

	return value
}
