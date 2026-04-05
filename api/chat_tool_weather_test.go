package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func decodeWeatherToolResult(t *testing.T, raw string) (chatToolResult, weatherToolData, string) {
	t.Helper()

	var result chatToolResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Unmarshal chatToolResult failed: %v", err)
	}

	dataBytes, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("Marshal result.Data failed: %v", err)
	}

	var summary weatherToolData
	if len(dataBytes) > 0 && string(dataBytes) != "null" {
		if err := json.Unmarshal(dataBytes, &summary); err != nil {
			t.Fatalf("Unmarshal weatherToolData failed: %v", err)
		}
	}

	return result, summary, string(dataBytes)
}

func assertWeatherToolDataMinimal(t *testing.T, dataJSON string) {
	t.Helper()

	forbiddenKeys := []string{
		"forecastDaily",
		"updateTime",
		"humidity",
		"pressure",
		"visibility",
		"wind",
		"weather_code",
		"day_code",
		"night_code",
		"precipitation",
	}

	for _, key := range forbiddenKeys {
		if strings.Contains(dataJSON, `"`+key+`"`) {
			t.Fatalf("tool data contains forbidden key %q: %s", key, dataJSON)
		}
	}
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
		label string
	}{
		{value: "50", label: "优"},
		{value: "85", label: "良"},
		{value: "120", label: "轻度污染"},
		{value: "180", label: "中度污染"},
		{value: "220", label: "重度污染"},
		{value: "320", label: "严重污染"},
	}

	for _, tc := range cases {
		got := buildWeatherAirQualitySummary(tc.value)
		if got == nil || got.Label != tc.label || got.Value != tc.value {
			t.Fatalf("buildWeatherAirQualitySummary(%q) = %#v, want value=%q label=%q", tc.value, got, tc.value, tc.label)
		}
	}
}

func TestGetChatTools_IncludesWeatherAndClawBotExcludesInteractiveTools(t *testing.T) {
	tools := getChatTools()
	foundWeather := false
	foundWriteMemoryByDefault := false
	for _, tool := range tools {
		if tool.Function.Name == "memory_batch_upsert" {
			t.Fatal("memory_batch_upsert should not be exposed to normal chat tools")
		}
		if tool.Function.Name == "get_weather" {
			foundWeather = true
		}
		if tool.Function.Name == "write_memory" {
			foundWriteMemoryByDefault = true
		}
	}
	if !foundWeather {
		t.Fatal("get_weather tool not registered")
	}
	if foundWriteMemoryByDefault {
		t.Fatal("write_memory should not be exposed without runtime availability")
	}

	writeMemoryTools := getChatTools(chatToolOptions{WriteMemoryEnabled: true})
	foundWriteMemory := false
	for _, tool := range writeMemoryTools {
		if tool.Function.Name == "write_memory" {
			foundWriteMemory = true
			break
		}
	}
	if !foundWriteMemory {
		t.Fatal("write_memory tool not registered when runtime availability is enabled")
	}

	clawBotTools := getChatTools(chatToolOptions{Channel: chatToolChannelClawBot})
	for _, tool := range clawBotTools {
		switch tool.Function.Name {
		case "get_time", "get_weather", "schedule_reminder":
		default:
			t.Fatalf("unexpected clawbot tool %q", tool.Function.Name)
		}
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
	if summary.SourceMode != "explicit" || summary.Query != "北京" {
		t.Fatalf("summary = %#v, want source_mode=explicit query=北京", summary)
	}
	if summary.Location != "北京, 中国" {
		t.Fatalf("location = %q, want %q", summary.Location, "北京, 中国")
	}
	if summary.Current.Text != "多云" {
		t.Fatalf("current.text = %q, want %q", summary.Current.Text, "多云")
	}
	if summary.Current.Temperature != "20°" || summary.Current.FeelsLike != "18°" {
		t.Fatalf("current = %#v, want formatted temperatures", summary.Current)
	}
	if summary.Today == nil || summary.Today.Day != "多云" || summary.Today.Night != "小雨" {
		t.Fatalf("today = %#v, want translated daily weather", summary.Today)
	}
	if summary.Today.TempLow != "12°" || summary.Today.TempHigh != "23°" {
		t.Fatalf("today = %#v, want temp_low=12° temp_high=23°", summary.Today)
	}
	if len(summary.Hourly) != 2 || summary.Hourly[1].Text != "小雨" {
		t.Fatalf("hourly = %#v, want translated hourly weather", summary.Hourly)
	}
	if summary.AirQuality == nil || summary.AirQuality.Label != "良" || summary.AirQuality.Value != "85" {
		t.Fatalf("air_quality = %#v, want value 85 label 良", summary.AirQuality)
	}
	assertWeatherToolDataMinimal(t, dataJSON)
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
	if summary.SourceMode != "default" || summary.Location != "上海, 中国" {
		t.Fatalf("summary = %#v, want default Shanghai", summary)
	}
	if summary.Rain.NextRainInMinutes == nil || *summary.Rain.NextRainInMinutes != 1 {
		t.Fatalf("rain = %#v, want next_rain_in_minutes=1", summary.Rain)
	}
}

func TestChatToolExecutor_GetWeatherTruncatesHourlyAndAlerts(t *testing.T) {
	weatherInfo := newTestWeatherInfo()
	weatherInfo.ForecastHourly.Temperature.Value = make([]float64, 30)
	weatherInfo.ForecastHourly.Weather.Value = make([]int, 30)
	for index := 0; index < 30; index++ {
		weatherInfo.ForecastHourly.Temperature.Value[index] = float64(index)
		weatherInfo.ForecastHourly.Weather.Value[index] = 1
	}
	weatherInfo.Alerts = make([]client.WeatherAlert, 5)
	for index := 0; index < 5; index++ {
		weatherInfo.Alerts[index] = client.WeatherAlert{
			Title:   "预警-" + strconv.Itoa(index),
			Level:   "yellow",
			PubTime: "2026-04-04T09:30:00+08:00",
		}
	}

	service := &stubWeatherService{weatherResult: weatherInfo}

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
		ID:   "call-weather-4",
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
	if len(summary.Hourly) > weatherMaxHourlyItems {
		t.Fatalf("hourly len = %d, want <= %d", len(summary.Hourly), weatherMaxHourlyItems)
	}
	if len(summary.Alerts) > weatherMaxAlerts {
		t.Fatalf("alerts len = %d, want <= %d", len(summary.Alerts), weatherMaxAlerts)
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
