package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

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

	// c.OnRequest(func(r *colly.Request) { r.Headers.Set("User-Agent", userAgentList[rand.Intn(len(userAgentList))]) })
	c.OnError(func(_ *colly.Response, err error) { log.Panicln("Something went wrong:", err) })
	// c.OnResponse(func(r *colly.Response) {})

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
	defer wg.Done()
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

	var wg sync.WaitGroup
	propertyUrls := make(chan string, pageCount)
	filteredProperties := make(chan string, propertyResultCount)
	propertyMetadata := make(chan Property, propertyResultCount)

	for range propertyResultCount {
		wg.Add(1)
		go getMetadataFromProperties(propertyMetadata, filteredProperties, &wg)
	}

	for range propertyResultCount {
		wg.Add(1)
		go filterProperties(filteredProperties, propertyUrls, &wg)
	}

	for i := range pageCount {
		wg.Add(1)
		go findProperties(i*pageLength, propertyUrls, &wg)
	}

	go func() {
		wg.Wait()
		close(propertyUrls)
		close(filteredProperties)
		close(propertyMetadata)
	}()

	for result := range filteredProperties {
		fmt.Println("Filtered property:", result)
	}
}
