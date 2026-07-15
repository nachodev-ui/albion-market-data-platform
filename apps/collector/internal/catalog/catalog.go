package catalog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"albion-market-data/collector/internal/domain"
)

type marketFile struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Markets       []domain.MarketDefinition `json:"markets"`
}

type Catalog struct {
	itemsByAlbionID map[int32]domain.ItemDimension
	itemsByID       map[string]domain.ItemDimension
	locations       map[string]domain.LocationDimension
	marketsByKey    map[string]domain.MarketDefinition
	markets         []domain.MarketDefinition
}

func Load(itemsPath, marketsPath string) (*Catalog, error) {
	catalog := &Catalog{
		itemsByAlbionID: make(map[int32]domain.ItemDimension),
		itemsByID:       make(map[string]domain.ItemDimension),
		locations:       make(map[string]domain.LocationDimension),
		marketsByKey:    make(map[string]domain.MarketDefinition),
	}
	if err := catalog.loadItems(itemsPath); err != nil {
		return nil, err
	}
	if err := catalog.loadMarkets(marketsPath); err != nil {
		return nil, err
	}
	return catalog, nil
}

func (c *Catalog) loadItems(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open item catalog: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			return fmt.Errorf("item catalog line %d has invalid format", lineNumber)
		}
		id64, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 32)
		if err != nil {
			return fmt.Errorf("item catalog line %d has invalid AlbionId: %w", lineNumber, err)
		}
		itemID := strings.TrimSpace(parts[1])
		name := ""
		if len(parts) == 3 {
			name = strings.TrimSpace(parts[2])
		}
		item := domain.ItemDimension{AlbionID: int32(id64), ID: itemID, Name: name}
		c.itemsByAlbionID[item.AlbionID] = item
		c.itemsByID[item.ID] = item
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan item catalog: %w", err)
	}
	if len(c.itemsByAlbionID) == 0 {
		return fmt.Errorf("item catalog is empty")
	}
	return nil
}

func (c *Catalog) loadMarkets(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read market catalog: %w", err)
	}
	var decoded marketFile
	if err := json.Unmarshal(content, &decoded); err != nil {
		return fmt.Errorf("decode market catalog: %w", err)
	}
	if decoded.SchemaVersion != 1 {
		return fmt.Errorf("unsupported market catalog schema version %d", decoded.SchemaVersion)
	}
	for index, entry := range decoded.Markets {
		entry.Key = strings.TrimSpace(strings.ToLower(entry.Key))
		entry.Name = strings.TrimSpace(entry.Name)
		entry.Type = strings.TrimSpace(entry.Type)
		entry.CityLocationID = strings.TrimSpace(entry.CityLocationID)
		if entry.MarketLocationID != nil {
			trimmed := strings.TrimSpace(*entry.MarketLocationID)
			entry.MarketLocationID = &trimmed
		}
		if entry.Key == "" || entry.Name == "" || entry.Type == "" || entry.CityLocationID == "" {
			return fmt.Errorf("market catalog entry %d is incomplete", index)
		}
		if entry.Type != "regular" && entry.Type != "black-market" {
			return fmt.Errorf("market catalog entry %d has unsupported type %q", index, entry.Type)
		}
		if entry.Enabled && (entry.MarketLocationID == nil || *entry.MarketLocationID == "") {
			return fmt.Errorf("enabled market %q requires marketLocationId", entry.Key)
		}
		if _, exists := c.marketsByKey[entry.Key]; exists {
			return fmt.Errorf("duplicated market key %q", entry.Key)
		}
		c.marketsByKey[entry.Key] = entry
		c.markets = append(c.markets, entry)
		location := domain.LocationDimension{ID: entry.CityLocationID, Name: entry.Name, MarketKey: entry.Key}
		if _, exists := c.locations[entry.CityLocationID]; !exists {
			c.locations[entry.CityLocationID] = location
		}
		if entry.MarketLocationID != nil && *entry.MarketLocationID != "" {
			if existing, exists := c.locations[*entry.MarketLocationID]; exists && existing.MarketKey != entry.Key {
				return fmt.Errorf("market location %q is assigned to both %q and %q", *entry.MarketLocationID, existing.MarketKey, entry.Key)
			}
			c.locations[*entry.MarketLocationID] = domain.LocationDimension{
				ID: *entry.MarketLocationID, Name: entry.Name, MarketKey: entry.Key,
			}
		}
	}
	if len(c.markets) == 0 {
		return fmt.Errorf("market catalog is empty")
	}
	return nil
}

func (c *Catalog) ItemByAlbionID(id int32) (domain.ItemDimension, bool) {
	item, ok := c.itemsByAlbionID[id]
	return item, ok
}

func (c *Catalog) ItemByID(id string) (domain.ItemDimension, bool) {
	item, ok := c.itemsByID[id]
	return item, ok
}

func (c *Catalog) Location(id string) domain.LocationDimension {
	if location, ok := c.locations[id]; ok {
		return location
	}
	return domain.LocationDimension{ID: id}
}

// CanonicalMarketLocation normalizes both the city location and the dedicated
// marketplace location to the marketplace ID published by the catalog.
//
// Albion Data Client can temporarily alternate between both IDs while the
// player enters a marketplace. Market orders and histories captured during
// that transition must still be queryable through the public marketKey.
//
// The Black Market is the exception: it has no separate marketplace ID and ADC
// reports Caerleon's city location (3003), while the regular Caerleon market is
// reported as 3005. Resolve that explicit catalog entry before applying regular
// city aliases so Black Market buy orders are never relabeled as Caerleon.
func (c *Catalog) CanonicalMarketLocation(id string) domain.LocationDimension {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.LocationDimension{}
	}

	for index := range c.markets {
		market := &c.markets[index]
		if market.Type == "black-market" && id == market.CityLocationID {
			return domain.LocationDimension{
				ID:        id,
				Name:      market.Name,
				MarketKey: market.Key,
			}
		}
	}

	var matched *domain.MarketDefinition
	for index := range c.markets {
		market := &c.markets[index]
		if !market.Enabled || market.MarketLocationID == nil || strings.TrimSpace(*market.MarketLocationID) == "" {
			continue
		}
		if id != market.CityLocationID && id != *market.MarketLocationID {
			continue
		}
		if matched != nil && matched.Key != market.Key {
			// Keep the original location if the alias ever becomes ambiguous.
			return c.Location(id)
		}
		matched = market
	}

	if matched == nil {
		return c.Location(id)
	}
	return domain.LocationDimension{
		ID:        *matched.MarketLocationID,
		Name:      matched.Name,
		MarketKey: matched.Key,
	}
}

func (c *Catalog) Market(key string) (domain.MarketDefinition, bool) {
	market, ok := c.marketsByKey[strings.TrimSpace(strings.ToLower(key))]
	return market, ok
}

func (c *Catalog) Markets(includeDisabled bool) []domain.MarketDefinition {
	result := make([]domain.MarketDefinition, 0, len(c.markets))
	for _, market := range c.markets {
		if !includeDisabled && !market.Enabled {
			continue
		}
		result = append(result, market)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func Quality(level uint8) (domain.QualityDimension, error) {
	names := map[uint8]string{
		1: "Normal",
		2: "Bueno",
		3: "Sobresaliente",
		4: "Excelente",
		5: "Obra maestra",
	}
	name, ok := names[level]
	if !ok {
		return domain.QualityDimension{}, fmt.Errorf("quality level must be between 1 and 5, got %d", level)
	}
	return domain.QualityDimension{ID: level, Name: name}, nil
}
