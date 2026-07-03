package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/storage/queryjsonl"
	"albion-market-data/collector/internal/upstream"
)

type openAPIDocument struct {
	OpenAPI    string                          `json:"openapi"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas map[string]openAPISchema `json:"schemas"`
	} `json:"components"`
}

type operation struct {
	OperationID string                     `json:"operationId"`
	Responses   map[string]openAPIResponse `json:"responses"`
}

type openAPIResponse struct {
	Content map[string]struct {
		Schema struct {
			Ref string `json:"$ref"`
		} `json:"schema"`
	} `json:"content"`
}

type openAPISchema struct {
	Properties map[string]json.RawMessage `json:"properties"`
}

func TestOpenAPIPathsMatchLocalRouter(t *testing.T) {
	document := readOpenAPI(t)
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("openapi=%q, want 3.1.0", document.OpenAPI)
	}

	gotPaths := sortedKeys(document.Paths)
	wantPaths := append([]string(nil), DocumentedGETRoutes...)
	sort.Strings(wantPaths)
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("OpenAPI paths=%v, router paths=%v", gotPaths, wantPaths)
	}

	wantResponses := map[string]string{
		RouteHealth:  "HealthResponse",
		RouteReady:   "ReadinessResponse",
		RouteStatus:  "StatusResponse",
		RouteMarkets: "MarketsResponse",
		RoutePrices:  "PricesResponse",
		RouteHistory: "HistoryResponse",
		RouteOrders:  "OrdersResponse",
	}
	for path, methods := range document.Paths {
		if len(methods) != 1 {
			t.Fatalf("%s methods=%v, want only get", path, sortedKeys(methods))
		}
		operation, ok := methods["get"]
		if !ok {
			t.Fatalf("%s has no GET operation", path)
		}
		if strings.TrimSpace(operation.OperationID) == "" {
			t.Fatalf("%s GET operationId is empty", path)
		}
		success, ok := operation.Responses["200"]
		if !ok {
			t.Fatalf("%s GET has no 200 response", path)
		}
		mediaType, ok := success.Content["application/json"]
		if !ok {
			t.Fatalf("%s GET 200 has no application/json response", path)
		}
		wantRef := "#/components/schemas/" + wantResponses[path]
		if mediaType.Schema.Ref != wantRef {
			t.Fatalf("%s GET 200 schema=%q, want %q", path, mediaType.Schema.Ref, wantRef)
		}
	}
	if _, ok := document.Paths[RouteReady]["get"].Responses["503"]; !ok {
		t.Fatal("/readyz must document 503 not-ready responses")
	}
}

func TestOpenAPISchemasMatchGoJSONContracts(t *testing.T) {
	document := readOpenAPI(t)
	types := map[string]reflect.Type{
		"ErrorResponse":            reflect.TypeOf(errorResponse{}),
		"HealthResponse":           reflect.TypeOf(healthResponse{}),
		"ReadinessChecks":          reflect.TypeOf(readinessChecks{}),
		"ReadinessResponse":        reflect.TypeOf(readinessResponse{}),
		"HistoryResponse":          reflect.TypeOf(historyResponse{}),
		"OrdersResponse":           reflect.TypeOf(ordersResponse{}),
		"PriceRow":                 reflect.TypeOf(priceRow{}),
		"PricesResponse":           reflect.TypeOf(pricesResponse{}),
		"MarketsResponse":          reflect.TypeOf(marketsResponse{}),
		"StatusResponse":           reflect.TypeOf(statusResponse{}),
		"ItemDimension":            reflect.TypeOf(domain.ItemDimension{}),
		"LocationDimension":        reflect.TypeOf(domain.LocationDimension{}),
		"MarketDefinition":         reflect.TypeOf(domain.MarketDefinition{}),
		"QualityDimension":         reflect.TypeOf(domain.QualityDimension{}),
		"NormalizedHistoryPoint":   reflect.TypeOf(domain.NormalizedHistoryPoint{}),
		"NormalizedHistorySummary": reflect.TypeOf(domain.NormalizedHistorySummary{}),
		"NormalizedHistory":        reflect.TypeOf(domain.NormalizedHistory{}),
		"NormalizedMarketOrder":    reflect.TypeOf(domain.NormalizedMarketOrder{}),
		"RepositoryStats":          reflect.TypeOf(queryjsonl.RepositoryStats{}),
		"QueueSnapshot":            reflect.TypeOf(upstream.QueueSnapshot{}),
		"OutboxPipelineSnapshot":   reflect.TypeOf(upstream.OutboxPipelineSnapshot{}),
		"TotalsSnapshot":           reflect.TypeOf(upstream.TotalsSnapshot{}),
		"HistoryTotalsSnapshot":    reflect.TypeOf(upstream.HistoryTotalsSnapshot{}),
		"LatencySnapshot":          reflect.TypeOf(upstream.LatencySnapshot{}),
		"ForwarderSnapshot":        reflect.TypeOf(upstream.ForwarderSnapshot{}),
		"HistoryForwarderSnapshot": reflect.TypeOf(upstream.HistoryForwarderSnapshot{}),
	}

	for name, goType := range types {
		schema, ok := document.Components.Schemas[name]
		if !ok {
			t.Errorf("OpenAPI schema %s is missing", name)
			continue
		}
		got := sortedKeys(schema.Properties)
		want := jsonFieldNames(goType)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("schema %s properties=%v, Go JSON fields=%v", name, got, want)
		}
	}
}

func readOpenAPI(t *testing.T) openAPIDocument {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "openapi", "openapi.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAPI %s: %v", path, err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse OpenAPI: %v", err)
	}
	return document
}

func jsonFieldNames(value reflect.Type) []string {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	fields := make([]string, 0, value.NumField())
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			name = field.Name
		}
		if name == "-" {
			continue
		}
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return fields
}

func sortedKeys[M ~map[string]V, V any](values M) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
