package api

import (
	"bytes"
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleConfig_WeatherDefaultCityRoundTrip(t *testing.T) {
	handler := &Handler{configManager: newTestProviderConfigManager(t, newTestProvider("provider-1"))}

	firstCity := config.WeatherCity{
		Name:        "北京",
		Affiliation: "北京, 中国",
		LocationKey: "weathercn:101010100",
		Latitude:    "39.9042",
		Longitude:   "116.4074",
	}
	secondCity := config.WeatherCity{
		Name:        "上海",
		Affiliation: "上海, 中国",
		LocationKey: "weathercn:101020100",
		Latitude:    "31.2304",
		Longitude:   "121.4737",
	}

	for _, city := range []config.WeatherCity{firstCity, secondCity} {
		body, err := json.Marshal(map[string]interface{}{
			"weather_default_city": city,
		})
		if err != nil {
			t.Fatalf("Marshal request failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodPut, "/management/config", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		handler.handleConfig(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	}

	savedCity := handler.configManager.GetWeatherDefaultCity()
	if savedCity == nil {
		t.Fatal("saved city = nil, want value")
	}
	if savedCity.Name != secondCity.Name || savedCity.LocationKey != secondCity.LocationKey {
		t.Fatalf("saved city = %#v, want %#v", savedCity, secondCity)
	}

	req := httptest.NewRequest(http.MethodGet, "/management/config", nil)
	rec := httptest.NewRecorder()
	handler.handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			WeatherDefaultCity *config.WeatherCity `json:"weather_default_city"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode response failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("response success = false, body=%s", rec.Body.String())
	}
	if resp.Data.WeatherDefaultCity == nil {
		t.Fatal("response weather_default_city = nil, want value")
	}
	if resp.Data.WeatherDefaultCity.Name != secondCity.Name || resp.Data.WeatherDefaultCity.LocationKey != secondCity.LocationKey {
		t.Fatalf("response city = %#v, want %#v", resp.Data.WeatherDefaultCity, secondCity)
	}
}
