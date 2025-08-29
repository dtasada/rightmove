package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
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
)

var (
	propertyResultCount uint32
	pageCount           uint32
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
			log.Printf("Rate limited (429). Waiting 10 seconds before continuing...\n")
			time.Sleep(10 * time.Second)
		} else {
			log.Printf("Request failed: %v\n", err)
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
			if s.Text() == "Page " {
				return
			}
			pageCount_, err := strconv.Atoi(strings.Split(s.Text(), " ")[1]) // this is not a space. this is ASCII 160
			if err != nil {
				log.Fatalln("Could not strconv.Atoi property result count: ", err)
			}
			pageCount = uint32(pageCount_)
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
	c.OnHTML(".STw8udCxUaBUMfOOZu0iL._3nPVwR0HZYQah5tkVJHFh5", func(e *colly.HTMLElement) {
		for _, kw := range config.Keywords {
			if strings.Contains(e.Text, kw) {
				filteredProperties <- e.Request.URL.String()
				break
			}
		}
	})

	c.OnResponse(func(r *colly.Response) { visitWg.Done() })

	for propertyUrl := range propertyUrls {
		fullPropertyUrl := rootUrl + propertyUrl
		visitWg.Add(1)
		c.Visit(fullPropertyUrl)
	}

	visitWg.Wait()
	log.Printf("Filter worker finished processing all property URLs")
}

func getMetadataFromProperties(propertyMetadata chan Property, filteredProperties <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()
	var getDataWg sync.WaitGroup

	c := createCollector()
	c.OnHTML(".WJG_W7faYk84nW-6sCBVi", func(e *colly.HTMLElement) {
		var p Property
		p.Url = e.Request.URL.String()

		p.Street = e.DOM.Find("[itemprop=streetAddress]").First().Text()

		e.DOM.Find("._1hV1kqpVceE9m-QrX_hWDN").Each(func(i int, s *goquery.Selection) {
			switch i {
			case 0:
				p.PropertyType = s.Text()
			case 1:
				beds, err := strconv.Atoi(s.Text())
				if err != nil {
					log.Fatalln("Couldn't convert Bedrooms to number: ", err)
				}
				p.Beds = beds
			case 2:
				baths, err := strconv.Atoi(s.Text())
				if err != nil {
					log.Fatalln("Couldn't convert Bathrooms to number: ", err)
				}
				p.Baths = baths
			case 3:
				size_string := s.Parent().Children().Last().Text()
				size, err := strconv.Atoi(strings.Split(size_string, " sq m")[0])
				if err != nil {
					log.Fatalln("Couldn't convert surface area to number: ", err)
				}
				p.Size = size
			case 4:
				p.Tenure = s.Text()
			}
		})

		propertyMetadata <- p
	})

	c.OnResponse(func(r *colly.Response) { getDataWg.Done() })

	for propertyUrl := range filteredProperties {
		getDataWg.Add(1)
		c.Visit(propertyUrl)
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
	config_file, err := os.ReadFile("config.yaml")
	if err != nil {
		log.Fatalf("Could not read config.yaml: %v\n", err)
	}
	if err := yaml.Unmarshal(config_file, &config); err != nil {
		log.Fatalf("Could not parse config.yaml: %v\n", err)
	}

	setMetadata()
	getPropertyCount()

	propertyUrls := make(chan string, pageCount)
	filteredProperties := make(chan string, propertyResultCount)
	propertyMetadata := make(chan Property, propertyResultCount)

	// Limit concurrent workers to reduce load on Rightmove
	maxWorkers := min(3, int(propertyResultCount))

	var filterWg sync.WaitGroup
	var metaWg sync.WaitGroup

	// Limited concurrent filter workers
	for i := 0; i < maxWorkers; i++ {
		filterWg.Add(1)
		go filterProperties(filteredProperties, propertyUrls, &filterWg)
	}

	// Limited concurrent metadata workers
	for range maxWorkers {
		metaWg.Add(1)
		go getMetadataFromProperties(propertyMetadata, filteredProperties, &metaWg)
	}

	// Process pages sequentially to avoid overwhelming the server
	go func() {
		for i := range pageCount {
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
	}()

	// Collect and display results
	for result := range filteredProperties {
		fmt.Println("Filtered property:", result)
	}

	log.Printf("Scraping completed successfully!")
}
