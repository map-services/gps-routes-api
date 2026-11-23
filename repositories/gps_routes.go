package repositories

import (
	"context"

	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
)

type DbRepository interface {
	Store(ctx context.Context, route *domain.RouteMetadata) error
	StoreBatch(ctx context.Context, routes []*domain.RouteMetadata) error
	FindByObjectID(ctx context.Context, objectID string) (*domain.RouteMetadata, error)
	CountAll(ctx context.Context, criteria *request.SearchRequest) (int64, error)
	SearchHits(ctx context.Context, criteria *request.SearchRequest) (*[]domain.RouteSummary, error)
	FacetCounts(ctx context.Context, criteria *request.SearchRequest, facetField string, limit int32, unnest bool, excludeFacets ...string) (*map[string]int64, error)
}
