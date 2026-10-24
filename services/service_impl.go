package services

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
	repo "github.com/map-services/gps-routes-api/repositories"
	"github.com/map-services/gps-routes-api/services/osdatahub"
)

var ATTRIBUTION = []string{
	"GPS Cycle and Walking Routes: https://gps-routes.co.uk",
	"OS DataHub: Names API. https://www.ordnancesurvey.co.uk",
	"Nominatim: Data © OpenStreetMap contributors, ODbL 1.0. http://osm.org/copyright",
	"Llama.cpp: https://github.com/ggml-org/llama.cpp/blob/master/LICENSE",
	"lmstudio-community/gemma-3-4b-it-Q8_0.gguf: https://ai.google.dev/gemma/terms",
}

type RoutesService interface {
	GetRouteByID(objectID string) (*domain.RouteMetadata, error)
	Search(criteria *request.SearchRequest) (*domain.SearchResults, error)
	RefData() (*domain.RefData, error)
}

type RoutesServiceImpl struct {
	repository    repo.DbRepository
	namesApi      osdatahub.NamesApi
	searchTimeout time.Duration
}

func (service *RoutesServiceImpl) GetRouteByID(objectID string) (*domain.RouteMetadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return service.repository.FindByObjectID(ctx, objectID)
}

type facet struct {
	Name   string
	Values map[string]int64
}

func (service *RoutesServiceImpl) Search(criteria *request.SearchRequest) (*domain.SearchResults, error) {
	ctx, cancel := context.WithTimeout(context.Background(), service.searchTimeout)
	defer cancel()

	if criteria.Nearby != nil && criteria.Nearby.Place != "" && criteria.Nearby.Center == nil {
		result, err := service.namesApi.Find(ctx, criteria.Nearby.Place)
		if err != nil {
			return nil, fmt.Errorf("error fetching from osdatahub Names API: %w", err)
		}
		criteria.Nearby.Center = osdatahub.ToWSG84(result)
	}

	totalChan := make(chan int64, 1)
	resultsChan := make(chan []domain.RouteSummary, 1)
	facetsChan := make(chan facet, len(FACET_FIELDS))
	errorChan := make(chan error, 2+len(FACET_FIELDS)) // 2 for total and results, plus the number of facets

	fetchCounts := func() {
		total, err := service.repository.CountAll(ctx, criteria)
		if err != nil {
			errorChan <- err
		} else {
			totalChan <- total
		}
	}

	fetchResults := func() {
		results, err := service.repository.SearchHits(ctx, criteria)
		if err != nil {
			errorChan <- err
		} else {
			resultsChan <- *results
		}
	}

	go fetchResults()
	go fetchCounts()

	var waitForFacets int

	if !criteria.SkipFacets {
		for fieldName, facetConfig := range FACET_FIELDS {
			go func(fn string, fc *FacetConfig) {
				results, err := service.repository.FacetCounts(ctx, criteria, fn, fc.Limit, fc.Unnest, fc.Excluded...)
				if err != nil {
					errorChan <- err
				} else {
					facetsChan <- facet{Name: fn, Values: *results}
				}
			}(fieldName, facetConfig)
			waitForFacets++
		}
	}

	var total int64
	var results []domain.RouteSummary

	facets := make(domain.Facets, waitForFacets)
	remaining := 2 + waitForFacets // 2 for total and results, plus the number of facets

	for remaining > 0 {
		select {
		case err := <-errorChan:
			return nil, err
		case total = <-totalChan:
			remaining--
		case results = <-resultsChan:
			remaining--
		case facet := <-facetsChan:
			remaining--
			facets[facet.Name] = facet.Values
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Determine the effective limit (defaults to 10 when 0)
	effectiveLimit := criteria.Limit
	if effectiveLimit == 0 {
		effectiveLimit = 10
	}

	hasMore := int64(criteria.Offset)+int64(effectiveLimit) < total
	nextOffset := int32(0)
	if hasMore {
		nextOffset = criteria.Offset + effectiveLimit
	}

	// Compute bounding box encompassing all hits
	var boundingBox *common.BoundingBox
	for i, hit := range results {
		if i == 0 {
			boundingBox = common.NewBoundingBox(hit.StartPosition)
		} else {
			boundingBox.Expand(hit.StartPosition)
		}
	}

	return &domain.SearchResults{
		Hits:        results,
		BoundingBox: boundingBox,
		Total:       total,
		Facets:      facets,
		Attribution: ATTRIBUTION,
		Offset:      criteria.Offset,
		Limit:       effectiveLimit,
		HasMore:     hasMore,
		NextOffset:  nextOffset,
	}, nil
}

func (service *RoutesServiceImpl) RefData() (*domain.RefData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), service.searchTimeout)
	defer cancel()

	facetsChan := make(chan facet, len(FACET_FIELDS))
	errorChan := make(chan error, len(FACET_FIELDS))
	emptyCriteria := request.SearchRequest{}

	for fieldName, facetConfig := range FACET_FIELDS {
		go func() {
			results, err := service.repository.FacetCounts(ctx, &emptyCriteria, fieldName, -1 /* no limit */, facetConfig.Unnest)
			if err != nil {
				errorChan <- err
			} else {
				facetsChan <- facet{Name: fieldName, Values: *results}
			}
		}()
	}

	facets := make(domain.Facets, len(FACET_FIELDS))
	remaining := len(FACET_FIELDS)

	for remaining > 0 {
		select {
		case err := <-errorChan:
			return nil, err
		case facet := <-facetsChan:
			remaining--
			facets[facet.Name] = facet.Values
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return &domain.RefData{
		Facets:      facets,
		Attribution: ATTRIBUTION,
	}, nil
}

// defaultSearchTimeout is the fallback when SEARCH_TIMEOUT_SECONDS is not set.
const defaultSearchTimeout = 10 * time.Second

// GetSearchTimeout reads the SEARCH_TIMEOUT_SECONDS environment variable and
// returns the parsed duration.  It falls back to 10 seconds when the variable
// is unset, empty, or contains an invalid value.
func GetSearchTimeout() time.Duration {
	val := os.Getenv("SEARCH_TIMEOUT_SECONDS")
	if val == "" {
		return defaultSearchTimeout
	}
	seconds, err := strconv.Atoi(val)
	if err != nil || seconds <= 0 {
		return defaultSearchTimeout
	}
	return time.Duration(seconds) * time.Second
}

func NewRoutesService(repo repo.DbRepository, namesApi osdatahub.NamesApi) *RoutesServiceImpl {
	return &RoutesServiceImpl{
		repository:    repo,
		namesApi:      namesApi,
		searchTimeout: GetSearchTimeout(),
	}
}
