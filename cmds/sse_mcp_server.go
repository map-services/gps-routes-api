package cmds

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func NewSseMcpServer(port int) {

	// Create MCP server
	s := server.NewMCPServer(
		"gps-routes",
		versioninfo.Short(),
		server.WithLogging(),
		server.WithRecovery(),
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
	)

	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	gpsRoutesTool := mcp.NewTool("gps_routes",
		mcp.WithDescription("Searches for walking and cycling routes in the United Kingdom given the query"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("keywords to search for")),
		mcp.WithString("nearby",
			mcp.Description("Limits the search to the specific area, used in conjunction with distanceKm")),
		mcp.WithString("distanceKm",
			mcp.Description("The search radius used when nearby searching, this should contain only numeric values"),
			mcp.DefaultNumber(50)),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint:    &readOnly,
			DestructiveHint: &destructive,
			IdempotentHint:  &idempotent,
			OpenWorldHint:   &openWorld,
		}),
	)

	refDataResources := mcp.NewResource(
		"ref-data://gps-routes",
		"Reference Data",
		mcp.WithMIMEType("application/json"),
		mcp.WithResourceDescription(
			"Reference data for the GPS Routes API, detailing all the facets and their possbile values. Note that the key represents the valid facet values, while the numeric values are the counts of how many instances there are. You should infer higher numbers mean more popular facets."),
	)

	s.AddResource(refDataResources, refDataHandler)
	s.AddTool(gpsRoutesTool, gpsRoutesHandler)

	log.Printf("Starting MCP SSE Server on port %d...", port)
	err := server.NewSSEServer(s, server.WithSSEEndpoint("/mcp")).Start(fmt.Sprintf(":%d", port))
	log.Fatalf("MCP SSE Server failed to start on port %d: %v", port, err)
}

// func McpMiddleware(sseServer *server.SSEServer) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		// c.Writer.Header().Set("content-type", "application/json; charset=utf-8")
// 		sseServer.ServeHTTP(c.Writer, c.Request)
// 	}
// }

func gpsRoutesHandler(ctx context.Context, toolRequest mcp.CallToolRequest) (*mcp.CallToolResult, error) {

	payload := request.SearchRequest{
		Query:        mcp.ParseString(toolRequest, "query", ""),
		Limit:        5,
		TruncateText: false,
		SkipFacets:   true,
	}
	nearby := mcp.ParseString(toolRequest, "nearby", "")
	if nearby != "" {
		payload.Nearby = &request.Nearby{
			Place:      nearby,
			DistanceKm: mcp.ParseInt32(toolRequest, "distanceKm", 20),
		}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshalling payload to JSON: %w", err)
	}

	baseUrl := "https://api.hz-nbg1.destructuring-bind.org/v1/gps-routes" // TODO: use env var
	req, err := http.NewRequestWithContext(ctx, "POST", baseUrl+"/search", bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("User-Agent", "MCP-GPS-Routes-API")
	req.Header.Add("Accept-Language", "en-GB,en;q=0.9,en-US;q=0.8")
	req.Header.Add("X-API-Key", os.Getenv("GPS_ROUTES_API_KEY"))

	log.Printf("Making request to %s", req.URL.String())
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	var searchResults domain.SearchResults
	err = json.Unmarshal(body, &searchResults)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response body: %w", err)
	}
	content := make([]mcp.Content, 0, 1+len(searchResults.Hits))
	content = append(content, mcp.TextContent{
		Type: "text",
		Text: "The following routes were found for the search query. Do not include information about any other " +
			"routes from other sources, just the ones listed below. Do not alter the display order. When summarizing, " +
			"pick out the most pertinent information and do not include any extraneous information. Use markdown to " +
			"format the text, including headings, bullet points, and if present, display associated images.",
		Annotated: mcp.Annotated{
			Annotations: &mcp.Annotations{
				Audience: []mcp.Role{"assistant"},
			},
		},
	})
	log.Printf("Found %d matching hits", len(searchResults.Hits))
	for idx, hit := range searchResults.Hits {
		// bytes, err := json.Marshal(hit)
		// if err != nil {
		// 	return nil, fmt.Errorf("error marshalling search hit to JSON: %w", err)
		// }
		log.Println(idx, ":", hit.Title)
		content = append(content, mcp.TextContent{
			Type: "text",
			Text: formattedText(hit),
		})

		if hit.HeadlineImageUrl != nil {
			contentType, imageBytes, err := fetchImage(*hit.HeadlineImageUrl)
			if err != nil {
				return nil, fmt.Errorf("error fetching image: %w", err)
			}
			log.Printf("Fetched image from %s (content-type=%s, size=%d)", *hit.HeadlineImageUrl, *contentType, len(imageBytes))
			content = append(content, mcp.ImageContent{
				Type:     "image",
				Data:     base64.StdEncoding.EncodeToString(imageBytes),
				MIMEType: *contentType,
			})
		}
	}

	return &mcp.CallToolResult{Content: content, IsError: false}, nil
}

func formattedText(hit domain.RouteSummary) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ObjectID: %s\n", hit.ObjectID))
	sb.WriteString(fmt.Sprintf("Title: %s\n", hit.Title))
	sb.WriteString(fmt.Sprintf("Description: %s\n", hit.Description))
	sb.WriteString(fmt.Sprintf("Distance: %.2f km\n", hit.DistanceKm))
	sb.WriteString(fmt.Sprintf("Latitude: %.6f\n", hit.StartPosition.Latitude))
	sb.WriteString(fmt.Sprintf("Longitude: %.6f\n", hit.StartPosition.Longitude))
	return sb.String()
}

func fetchImage(uri string) (*string, []byte, error) {
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Add("Accept", "image/*")
	req.Header.Add("User-Agent", "MCP-GPS-Routes-API")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("error making request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	contentType := resp.Header.Get("Content-Type")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading response body: %w", err)
	}

	return &contentType, body, nil
}

func refDataHandler(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {

	baseUrl := "https://api.hz-nbg1.destructuring-bind.org/v1/gps-routes" // TODO: use env var
	req, err := http.NewRequestWithContext(ctx, "GET", baseUrl+"/ref-data", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)

	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("User-Agent", "MCP-GPS-Routes-API")
	req.Header.Add("Accept-Language", "en-GB,en;q=0.9,en-US;q=0.8")
	req.Header.Add("X-API-Key", os.Getenv("GPS_ROUTES_API_KEY"))

	log.Printf("Making request to %s", req.URL.String())
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	return []mcp.ResourceContents{mcp.TextResourceContents{
		URI:      request.Params.URI,
		MIMEType: "application/json",
		Text:     string(body),
	}}, nil
}
