package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubWeatherService struct {
	searchCitiesResult []client.WeatherCity
	searchCitiesErr    error
	searchKeywords     []string

	cityInfoResult *client.WeatherCity
	cityInfoErr    error
	cityInfoKeys   []string

	weatherResult   *client.WeatherInfo
	weatherErr      error
	weatherRequests []weatherRequestRecord
}

type weatherRequestRecord struct {
	locationKey string
	latitude    string
	longitude   string
}

func (s *stubWeatherService) SearchCities(ctx context.Context, keyword string) ([]client.WeatherCity, error) {
	s.searchKeywords = append(s.searchKeywords, keyword)
	return s.searchCitiesResult, s.searchCitiesErr
}

func (s *stubWeatherService) GetCityInfo(ctx context.Context, locationKey string) (*client.WeatherCity, error) {
	s.cityInfoKeys = append(s.cityInfoKeys, locationKey)
	return s.cityInfoResult, s.cityInfoErr
}

func (s *stubWeatherService) GetWeather(ctx context.Context, locationKey, latitude, longitude string) (*client.WeatherInfo, error) {
	s.weatherRequests = append(s.weatherRequests, weatherRequestRecord{
		locationKey: locationKey,
		latitude:    latitude,
		longitude:   longitude,
	})
	return s.weatherResult, s.weatherErr
}

func newTestWeatherInfo() *client.WeatherInfo {
	return &client.WeatherInfo{
		Current: client.CurrentWeather{
			FeelsLike:   client.WeatherMetric{Value: "18", Unit: "°"},
			Humidity:    client.WeatherMetric{Value: "68", Unit: "%"},
			Pressure:    client.WeatherMetric{Value: "1012", Unit: "hPa"},
			Temperature: client.WeatherMetric{Value: "20", Unit: "°"},
			Visibility:  client.WeatherMetric{Value: "10", Unit: "km"},
			Weather:     "1",
			PubTime:     "2026-04-04T10:00:00+08:00",
			Wind: client.WeatherWind{
				Direction: client.WeatherMetric{Value: "90", Unit: "°"},
				Speed:     client.WeatherMetric{Value: "3", Unit: "m/s"},
			},
		},
		Alerts: []client.WeatherAlert{
			{
				Title:   "暴雨黄色预警",
				Type:    "rainstorm",
				Level:   "yellow",
				Detail:  "未来6小时内部分地区将出现强降雨。",
				PubTime: "2026-04-04T09:30:00+08:00",
			},
		},
		UpdateTime: 1710000000000,
		ForecastDaily: client.ForecastDaily{
			PrecipitationProbability: client.StringArrayValue{Value: []string{"20%"}},
			Temperature: client.DailyTemperatureValue{
				Value: []client.DailyTemperatureRange{{From: "12", To: "23"}},
			},
			Weather: client.DailyWeatherValue{
				Value: []client.DailyWeatherRange{{From: "1", To: "7"}},
			},
			SunRiseSet: client.SunRiseSetValue{
				Value: []client.SunRiseSetRange{{
					From: "2026-04-04T05:58:00+08:00",
					To:   "2026-04-04T18:32:00+08:00",
				}},
			},
		},
		ForecastHourly: client.ForecastHourly{
			Temperature: client.FloatArrayValue{Value: []float64{20, 21}},
			Weather:     client.IntArrayValue{Value: []int{1, 7}},
		},
		Minutely: client.Minutely{
			Precipitation: client.FloatArrayValue{Value: []float64{0, 0.2, 0.4, 0}},
		},
		AQI: client.AQI{AQI: "85"},
	}
}

func decodeWeatherToolResult(t *testing.T, raw string) (chatToolResult, weatherToolSummary, string) {
	t.Helper()

	var result chatToolResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Unmarshal chatToolResult failed: %v", err)
	}

	dataBytes, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("Marshal result.Data failed: %v", err)
	}

	var summary weatherToolSummary
	if len(dataBytes) > 0 && string(dataBytes) != "null" {
		if err := json.Unmarshal(dataBytes, &summary); err != nil {
			t.Fatalf("Unmarshal weatherToolSummary failed: %v", err)
		}
	}

	return result, summary, string(dataBytes)
}

func TestTranslateWeatherCode(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{code: "0", want: "晴"},
		{code: "1", want: "多云"},
		{code: "7", want: "小雨"},
		{code: "301", want: "雨"},
	}

	for _, tc := range cases {
		if got := translateWeatherCode(tc.code); got != tc.want {
			t.Fatalf("translateWeatherCode(%q) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestTranslateWeatherCodeUnknownFallback(t *testing.T) {
	if got := translateWeatherCode("999"); got != "未知" {
		t.Fatalf("translateWeatherCode(999) = %q, want %q", got, "未知")
	}
}

func TestBuildWeatherAirQualitySummary(t *testing.T) {
	cases := []struct {
		value string
		level int
		label string
	}{
		{value: "50", level: 1, label: "优"},
		{value: "85", level: 2, label: "良"},
		{value: "120", level: 3, label: "轻度污染"},
		{value: "180", level: 4, label: "中度污染"},
		{value: "220", level: 5, label: "重度污染"},
		{value: "320", level: 6, label: "严重污染"},
	}

	for _, tc := range cases {
		got := buildWeatherAirQualitySummary(tc.value)
		if got.Level != tc.level || got.Label != tc.label {
			t.Fatalf("buildWeatherAirQualitySummary(%q) = (%d, %q), want (%d, %q)", tc.value, got.Level, got.Label, tc.level, tc.label)
		}
	}
}

func TestGetChatTools_IncludesWeatherButKeepsClawBotDisabled(t *testing.T) {
	tools := getChatTools()
	foundWeather := false
	for _, tool := range tools {
		if tool.Function.Name == "get_weather" {
			foundWeather = true
			break
		}
	}
	if !foundWeather {
		t.Fatal("get_weather tool not registered")
	}

	if tools := getChatTools(chatToolOptions{Channel: chatToolChannelClawBot}); tools != nil {
		t.Fatalf("clawbot tools = %#v, want nil", tools)
	}
}

func TestChatToolExecutor_GetWeatherWithExplicitCity(t *testing.T) {
	service := &stubWeatherService{
		searchCitiesResult: []client.WeatherCity{{
			Name:        "北京",
			Affiliation: "北京, 中国",
			LocationKey: "weathercn:101010100",
		}},
		cityInfoResult: &client.WeatherCity{
			Name:        "北京",
			Affiliation: "北京, 中国",
			LocationKey: "weathercn:101010100",
			Latitude:    "39.9042",
			Longitude:   "116.4074",
		},
		weatherResult: newTestWeatherInfo(),
	}

	executor := newChatToolExecutor(nil, nil)
	executor.configManager = newTestProviderConfigManager(t, newTestProvider("provider-1"))
	executor.weatherService = service

	raw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call-weather-1",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "get_weather",
			Arguments: `{"city":"北京"}`,
		},
	}, chatToolContext{})

	result, summary, dataJSON := decodeWeatherToolResult(t, raw)
	if !result.OK {
		t.Fatalf("result.OK = false, error=%q", result.Error)
	}
	if len(service.searchKeywords) != 1 || service.searchKeywords[0] != "北京" {
		t.Fatalf("searchKeywords = %#v, want [北京]", service.searchKeywords)
	}
	if len(service.cityInfoKeys) != 1 || service.cityInfoKeys[0] != "weathercn:101010100" {
		t.Fatalf("cityInfoKeys = %#v, want [weathercn:101010100]", service.cityInfoKeys)
	}
	if len(service.weatherRequests) != 1 {
		t.Fatalf("weatherRequests len = %d, want 1", len(service.weatherRequests))
	}
	if summary.SourceCity.Mode != "explicit" || summary.SourceCity.Query != "北京" {
		t.Fatalf("source_city = %#v, want explicit query 北京", summary.SourceCity)
	}
	if summary.Current.WeatherText != "多云" {
		t.Fatalf("current.weather_text = %q, want %q", summary.Current.WeatherText, "多云")
	}
	if summary.Today == nil || summary.Today.Weather.DayText != "多云" || summary.Today.Weather.NightText != "小雨" {
		t.Fatalf("today summary = %#v, want translated daily weather", summary.Today)
	}
	if len(summary.Hourly) != 2 || summary.Hourly[1].WeatherText != "小雨" {
		t.Fatalf("hourly summary = %#v, want translated hourly weather", summary.Hourly)
	}
	if summary.AirQuality.Level != 2 || summary.AirQuality.Label != "良" {
		t.Fatalf("air quality = %#v, want level 2 label 良", summary.AirQuality)
	}
	if strings.Contains(dataJSON, "forecastDaily") || strings.Contains(dataJSON, "updateTime") || strings.Contains(dataJSON, `"aqi":{"aqi"`) {
		t.Fatalf("tool data contains raw weather payload: %s", dataJSON)
	}
}

func TestChatToolExecutor_GetWeatherUsesDefaultCity(t *testing.T) {
	service := &stubWeatherService{
		weatherResult: newTestWeatherInfo(),
	}

	configManager := newTestProviderConfigManager(t, newTestProvider("provider-1"))
	cfg := configManager.Get()
	cfg.WeatherDefaultCity = &config.WeatherCity{
		Name:        "上海",
		Affiliation: "上海, 中国",
		LocationKey: "weathercn:101020100",
		Latitude:    "31.2304",
		Longitude:   "121.4737",
	}
	if err := configManager.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	executor := newChatToolExecutor(nil, nil)
	executor.configManager = configManager
	executor.weatherService = service

	raw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call-weather-2",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "get_weather",
			Arguments: `{}`,
		},
	}, chatToolContext{})

	result, summary, _ := decodeWeatherToolResult(t, raw)
	if !result.OK {
		t.Fatalf("result.OK = false, error=%q", result.Error)
	}
	if len(service.searchKeywords) != 0 || len(service.cityInfoKeys) != 0 {
		t.Fatalf("default city should not trigger search/info calls: search=%#v cityInfo=%#v", service.searchKeywords, service.cityInfoKeys)
	}
	if len(service.weatherRequests) != 1 {
		t.Fatalf("weatherRequests len = %d, want 1", len(service.weatherRequests))
	}
	if summary.SourceCity.Mode != "default" || summary.City != "上海" {
		t.Fatalf("summary = %#v, want default Shanghai", summary)
	}
	if summary.Minutely.NextRainInMinutes == nil || *summary.Minutely.NextRainInMinutes != 1 {
		t.Fatalf("minutely = %#v, want next_rain_in_minutes=1", summary.Minutely)
	}
}

func TestChatToolExecutor_GetWeatherRequiresDefaultCityWhenCityMissing(t *testing.T) {
	executor := newChatToolExecutor(nil, nil)
	executor.configManager = newTestProviderConfigManager(t, newTestProvider("provider-1"))
	executor.weatherService = &stubWeatherService{}

	raw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call-weather-3",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "get_weather",
			Arguments: `{}`,
		},
	}, chatToolContext{})

	result, _, _ := decodeWeatherToolResult(t, raw)
	if result.OK {
		t.Fatal("result.OK = true, want false")
	}
	if result.Error != "default weather city is not configured" {
		t.Fatalf("error = %q, want %q", result.Error, "default weather city is not configured")
	}
}

func TestHandleWeatherCitySearch(t *testing.T) {
	handler := &Handler{
		weatherService: &stubWeatherService{
			searchCitiesResult: []client.WeatherCity{
				{
					Name:        "北京",
					Affiliation: "北京, 中国",
					LocationKey: "weathercn:101010100",
					Latitude:    "39.9042",
					Longitude:   "116.4074",
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/management/weather/cities/search?q=北京", nil)
	rec := httptest.NewRecorder()

	handler.handleWeatherCitySearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool                 `json:"success"`
		Data    []config.WeatherCity `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode response failed: %v", err)
	}
	if !resp.Success || len(resp.Data) != 1 {
		t.Fatalf("response = %#v, want one city", resp)
	}
	if resp.Data[0].LocationKey != "weathercn:101010100" {
		t.Fatalf("location_key = %q, want %q", resp.Data[0].LocationKey, "weathercn:101010100")
	}
}
