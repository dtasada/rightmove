# Rightmove Property Scraper

A Go-based web scraper that searches for properties on Rightmove (UK property website) based on configurable criteria and filters results by keywords.

## Features

- Search properties by postcode/zip code and radius
- Filter by minimum number of bedrooms
- Filter results by custom keywords (e.g., "self-contained", "apartment")
- Concurrent scraping for improved performance
- Extracts detailed property information including address, type, beds, baths, size, and tenure

## Prerequisites

- Go 1.21 or higher
- Internet connection (for scraping Rightmove)

## Installation

1. Clone or download this repository
2. Navigate to the project directory:
   ```bash
   cd rightmove
   ```

3. Install dependencies:
   ```bash
   go mod tidy
   ```

## Configuration

Before running the scraper, you need to configure the search parameters in `config.yaml`:

```yaml
zipCode: SR2 7SB        # UK postcode to search around
radius: 10              # Search radius in miles
minBedrooms: 5          # Minimum number of bedrooms
keywords:               # Properties must contain at least one of these keywords
  - self-contained
  - self contained
  - apartment
  - flat
  - individual
```

### Configuration Options

- `zipCode`: UK postcode or area to search around (required)
- `radius`: Search radius in miles (required)
- `minBedrooms`: Minimum number of bedrooms (required)
- `keywords`: List of keywords to filter properties (required)

## Compilation and Execution

### Option 1: Run directly (recommended for development)

```bash
go run .
```

### Option 2: Compile and run

1. **Compile the application:**
   ```bash
   go build -o rightmove-scraper
   ```

2. **Run the compiled binary:**
   ```bash
   ./rightmove-scraper
   ```

### Option 3: Cross-compilation for different platforms

For Linux:
```bash
GOOS=linux GOARCH=amd64 go build -o rightmove-scraper-linux
```

For Windows:
```bash
GOOS=windows GOARCH=amd64 go build -o rightmove-scraper.exe
```

For macOS (ARM):
```bash
GOOS=darwin GOARCH=arm64 go build -o rightmove-scraper-mac-arm64
```

## Usage

1. **Ensure `config.yaml` is properly configured** with your search criteria
2. **Run the application** using one of the methods above
3. **The scraper will:**
   - Search for properties matching your criteria
   - Filter results based on your keywords
   - Display filtered properties with their URLs

### Example Output

```
Filtered property: https://www.rightmove.co.uk/properties/123456789
Filtered property: https://www.rightmove.co.uk/properties/987654321
...
```

## How It Works

1. **Configuration Loading**: Reads search parameters from `config.yaml`
2. **Metadata Setup**: Resolves the postcode to Rightmove's internal ID
3. **Property Discovery**: Scrapes property listings matching the search criteria
4. **Keyword Filtering**: Filters properties containing specified keywords in their descriptions
5. **Results Output**: Displays URLs of properties that match all criteria

## Project Structure

```
rightmove/
├── main.go          # Main application logic
├── config.yaml      # Search configuration
├── go.mod          # Go module definition
├── go.sum          # Go module checksums
└── README.md       # This documentation
```

## Dependencies

- `github.com/gocolly/colly` - Web scraping framework
- `github.com/PuerkitoBio/goquery` - HTML parsing and manipulation
- `github.com/goccy/go-yaml` - YAML configuration parsing

## Important Notes

- **Respectful Scraping**: The scraper includes user agent rotation and follows good scraping practices
- **Rate Limiting**: Consider the impact on Rightmove's servers and use responsibly
- **Legal Compliance**: Ensure compliance with Rightmove's terms of service and applicable laws
- **Error Handling**: The application will terminate with clear error messages if configuration is invalid or network issues occur

## Troubleshooting

### Common Issues

1. **Config file not found**:
   ```
   Could not read config.yaml: no such file or directory
   ```
   **Solution**: Ensure `config.yaml` exists in the same directory as the executable

2. **Invalid postcode**:
   ```
   Could not GET los.rightmove.co.uk: ...
   ```
   **Solution**: Verify the postcode in `config.yaml` is a valid UK postcode

3. **Network connectivity issues**:
   **Solution**: Check your internet connection and ensure Rightmove is accessible

4. **No results found**:
   **Solution**: Try broadening your search criteria (increase radius, reduce minimum bedrooms, or modify keywords)

5. **Build/run errors like `undefined: ProgressBar` or helpers**:
   ```
   # command-line-arguments
   ./main.go:XX: undefined: ProgressBar
   ```
   **Solution**: Use `go run .` or `go build` instead of `go run main.go`. The code spans multiple files; running a single file excludes the rest.

## License

This project is for educational and personal use only. Please respect Rightmove's terms of service and use responsibly.
