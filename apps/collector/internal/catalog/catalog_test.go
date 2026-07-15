package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMarketsPreservesLeadingZeroLocationIDs(t *testing.T) {
	directory := t.TempDir()
	items := filepath.Join(directory, "items.txt")
	markets := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(items, []byte("1: T3_CLOTH : Neat Cloth\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `{"schemaVersion":1,"markets":[{"key":"thetford","name":"Thetford","type":"regular","cityLocationId":"0000","marketLocationId":"0007","enabled":true}]}`
	if err := os.WriteFile(markets, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(items, markets)
	if err != nil {
		t.Fatal(err)
	}
	market, ok := loaded.Market("THETFORD")
	if !ok {
		t.Fatal("Thetford market was not loaded")
	}
	if market.CityLocationID != "0000" || market.MarketLocationID == nil || *market.MarketLocationID != "0007" {
		t.Fatalf("unexpected market: %+v", market)
	}
	location := loaded.Location("0007")
	if location.Name != "Thetford" || location.MarketKey != "thetford" {
		t.Fatalf("unexpected location: %+v", location)
	}
}

func TestDisabledBlackMarketDoesNotOverrideCaerleonCityLocation(t *testing.T) {
	directory := t.TempDir()
	items := filepath.Join(directory, "items.txt")
	markets := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(items, []byte("1: T3_CLOTH : Neat Cloth\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `{"schemaVersion":1,"markets":[{"key":"caerleon","name":"Caerleon","type":"regular","cityLocationId":"3003","marketLocationId":"3005","enabled":true},{"key":"black_market","name":"Black Market","type":"black-market","cityLocationId":"3003","marketLocationId":null,"enabled":false}]}`
	if err := os.WriteFile(markets, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(items, markets)
	if err != nil {
		t.Fatal(err)
	}
	location := loaded.Location("3003")
	if location.MarketKey != "caerleon" {
		t.Fatalf("shared city location was overridden: %+v", location)
	}
}

func TestCanonicalMarketLocationMapsCityAliasToMarketplace(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	items := filepath.Join(directory, "items.txt")
	markets := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(items, []byte("1070:T5_PLANKS_LEVEL4@4:Tablas de cedro\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `{"schemaVersion":1,"markets":[{"key":"fort_sterling","name":"Fort Sterling","type":"regular","cityLocationId":"4000","marketLocationId":"4002","enabled":true}]}`
	if err := os.WriteFile(markets, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(items, markets)
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{"4000", "4002"} {
		location := loaded.CanonicalMarketLocation(input)
		if location.ID != "4002" || location.Name != "Fort Sterling" || location.MarketKey != "fort_sterling" {
			t.Fatalf("CanonicalMarketLocation(%q) = %+v", input, location)
		}
	}
}

func TestCanonicalMarketLocationDistinguishesBlackMarketFromCaerleonMarketplace(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	items := filepath.Join(directory, "items.txt")
	markets := filepath.Join(directory, "markets.json")
	if err := os.WriteFile(items, []byte("1070:T5_PLANKS_LEVEL4@4:Tablas de cedro\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `{"schemaVersion":1,"markets":[{"key":"caerleon","name":"Caerleon","type":"regular","cityLocationId":"3003","marketLocationId":"3005","enabled":true},{"key":"black_market","name":"Black Market","type":"black-market","cityLocationId":"3003","marketLocationId":null,"enabled":false}]}`
	if err := os.WriteFile(markets, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(items, markets)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input     string
		wantID    string
		wantName  string
		wantKey   string
	}{
		{input: "3003", wantID: "3003", wantName: "Black Market", wantKey: "black_market"},
		{input: "3005", wantID: "3005", wantName: "Caerleon", wantKey: "caerleon"},
	}
	for _, test := range tests {
		location := loaded.CanonicalMarketLocation(test.input)
		if location.ID != test.wantID || location.Name != test.wantName || location.MarketKey != test.wantKey {
			t.Fatalf("CanonicalMarketLocation(%q) = %+v, want id=%q name=%q key=%q", test.input, location, test.wantID, test.wantName, test.wantKey)
		}
	}
}
