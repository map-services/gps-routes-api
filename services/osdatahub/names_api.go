package osdatahub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/kofalt/go-memoize"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Payload struct {
	Header  Header   `json:"header"`
	Results []Result `json:"results"`
}

type Header struct {
	URI          string `json:"uri"`
	Query        string `json:"query"`
	Format       string `json:"format"`
	MaxResults   int32  `json:"maxresults"`
	Offset       int32  `json:"offset"`
	TotalResults int32  `json:"totalresults"`
}

type ErrorPayload struct {
	Fault Fault `json:"fault"`
}

type Fault struct {
	FaultString string `json:"faultstring"`
	Detail      struct {
		ErrorCode string `json:"errorcode"`
	} `json:"detail"`
}

type Result struct {
	GazetteerEntry GazetteerEntry `json:"GAZETTEER_ENTRY"`
}

type GazetteerEntry struct {
	ID                  string  `json:"ID"`
	NamesURI            string  `json:"NAMES_URI"`
	Name1               string  `json:"NAME1"`
	Type                string  `json:"TYPE"`
	LocalType           string  `json:"LOCAL_TYPE"`
	GeometryX           float64 `json:"GEOMETRY_X"`
	GeometryY           float64 `json:"GEOMETRY_Y"`
	MostDetailViewRes   int32   `json:"MOST_DETAIL_VIEW_RES"`
	LeastDetailViewRes  int32   `json:"LEAST_DETAIL_VIEW_RES"`
	MbrXmin             float64 `json:"MBR_XMIN"`
	MbrYmin             float64 `json:"MBR_YMIN"`
	MbrXmax             float64 `json:"MBR_XMAX"`
	MbrYmax             float64 `json:"MBR_YMAX"`
	PostcodeDistrict    string  `json:"POSTCODE_DISTRICT"`
	PostcodeDistrictURI string  `json:"POSTCODE_DISTRICT_URI"`
	PopulatedPlace      string  `json:"POPULATED_PLACE"`
	PopulatedPlaceURI   string  `json:"POPULATED_PLACE_URI"`
	PopulatedPlaceType  string  `json:"POPULATED_PLACE_TYPE"`
	CountyUnitary       string  `json:"COUNTY_UNITARY"`
	CountyUnitaryURI    string  `json:"COUNTY_UNITARY_URI"`
	CountyUnitaryType   string  `json:"COUNTY_UNITARY_TYPE"`
	Region              string  `json:"REGION"`
	RegionURI           string  `json:"REGION_URI"`
	Country             string  `json:"COUNTRY"`
	CountryURI          string  `json:"COUNTRY_URI"`
}

type NamesApi interface {
	Find(ctx context.Context, name string) (*Result, error)
}

const metricsNamespace = "gps_routes"

var (
	namesApiCacheStats = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "names_api_cache_stats_total",
		Help:      "Number of calls to OS Names API cache statistics (hits & misses)",
	}, []string{"type"})
	namesApiCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "names_api_cache_size",
		Help:      "OS Names cache size (number of items)",
	})
)

type NamesApiImpl struct {
	baseUrl string
	apiKey  string
	cache   *memoize.Memoizer
}

func NewNamesApi(baseUrl string, apiKey string) *NamesApiImpl {
	return &NamesApiImpl{
		baseUrl: baseUrl,
		apiKey:  apiKey,
		cache:   memoize.NewMemoizer(8*time.Hour, 24*time.Hour),
	}
}

func (service *NamesApiImpl) Find(ctx context.Context, name string) (*Result, error) {
	result, err, cached := memoize.Call(service.cache, name, func() (*Result, error) {
		return service.fetch(ctx, name)
	})
	service.updateMetrics(cached)
	return result, err
}

func (service *NamesApiImpl) updateMetrics(cached bool) {
	namesApiCacheSize.Set(float64(service.cache.Storage.ItemCount()))
	if cached {
		namesApiCacheStats.WithLabelValues("hit").Inc()
	} else {
		namesApiCacheStats.WithLabelValues("miss").Inc()
	}
}

func (service *NamesApiImpl) fetch(ctx context.Context, name string) (*Result, error) {

	req, err := http.NewRequestWithContext(ctx, "GET", service.baseUrl+"/find", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	q := req.URL.Query()
	q.Add("maxresults", "1")
	q.Add("format", "JSON")
	q.Add("query", name)
	q.Add("key", service.apiKey)
	req.URL.RawQuery = q.Encode()

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

	if resp.StatusCode != 200 {
		var errorPayload ErrorPayload
		err = json.Unmarshal(body, &errorPayload)
		if err != nil {
			return nil, fmt.Errorf("error parsing fault JSON: %w", err)
		}

		return nil, fmt.Errorf("bad response (%s): %s", resp.Status, errorPayload.Fault.FaultString)
	}

	var payload Payload
	err = json.Unmarshal(body, &payload)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	if payload.Header.TotalResults == 0 {
		return nil, nil
	}

	return &payload.Results[0], nil
}
