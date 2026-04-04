package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	weatherMaxHourlyItems = 24
	weatherMaxAlerts      = 3
)

type weatherService interface {
	SearchCities(ctx context.Context, keyword string) ([]client.WeatherCity, error)
	GetCityInfo(ctx context.Context, locationKey string) (*client.WeatherCity, error)
	GetWeather(ctx context.Context, locationKey, latitude, longitude string) (*client.WeatherInfo, error)
}

type chatToolGetWeatherArgs struct {
	City string `json:"city,omitempty"`
}

type weatherToolData struct {
	Location   string                 `json:"location"`
	SourceMode string                 `json:"source_mode"`
	Query      string                 `json:"query,omitempty"`
	UpdatedAt  string                 `json:"updated_at"`
	Current    weatherCurrentData     `json:"current"`
	AirQuality *weatherAirQualityData `json:"air_quality,omitempty"`
	Alerts     []weatherAlertData     `json:"alerts,omitempty"`
	Today      *weatherTodayData      `json:"today,omitempty"`
	Hourly     []weatherHourlyData    `json:"hourly,omitempty"`
	Rain       weatherRainData        `json:"rain"`
}

type weatherCurrentData struct {
	Text        string `json:"text"`
	Temperature string `json:"temperature"`
	FeelsLike   string `json:"feels_like,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type weatherAirQualityData struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type weatherAlertData struct {
	Title       string `json:"title"`
	Level       string `json:"level,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type weatherTodayData struct {
	Day                      string `json:"day"`
	Night                    string `json:"night"`
	TempLow                  string `json:"temp_low,omitempty"`
	TempHigh                 string `json:"temp_high,omitempty"`
	PrecipitationProbability string `json:"precipitation_probability,omitempty"`
	SunRise                  string `json:"sun_rise,omitempty"`
	SunSet                   string `json:"sun_set,omitempty"`
}

type weatherHourlyData struct {
	OffsetHour  int     `json:"offset_hour"`
	Temperature float64 `json:"temperature"`
	Text        string  `json:"text"`
}

type weatherRainData struct {
	NextRainInMinutes *int  `json:"next_rain_in_minutes"`
	RainEndInMinutes  *int  `json:"rain_end_in_minutes"`
	RainingNow        *bool `json:"raining_now,omitempty"`
}

var weatherCodeTextMap = map[string]string{
	"0":   "晴",
	"1":   "多云",
	"2":   "阴",
	"3":   "阵雨",
	"4":   "雷阵雨",
	"7":   "小雨",
	"8":   "中雨",
	"9":   "大雨",
	"13":  "阵雪",
	"14":  "小雪",
	"18":  "雾",
	"53":  "霾",
	"99":  "未知",
	"301": "雨",
	"302": "雪",
}

func (h *Handler) getWeatherService() weatherService {
	if h.weatherService != nil {
		return h.weatherService
	}
	h.weatherService = client.NewWeatherClient("")
	return h.weatherService
}

func (h *Handler) handleWeatherCitySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if keyword == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "q is required"})
		return
	}

	service := h.getWeatherService()
	if service == nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Weather service not configured"})
		return
	}

	cities, errSearch := service.SearchCities(r.Context(), keyword)
	if errSearch != nil {
		h.jsonResponse(w, http.StatusBadGateway, Response{Success: false, Error: errSearch.Error()})
		return
	}

	results := make([]config.WeatherCity, 0, len(cities))
	for _, city := range cities {
		candidate := sanitizeWeatherConfigCity(config.WeatherCity{
			Name:        city.Name,
			Affiliation: city.Affiliation,
			LocationKey: city.LocationKey,
			Latitude:    city.Latitude,
			Longitude:   city.Longitude,
		})
		if errValidate := validateWeatherConfigCity(candidate); errValidate != nil {
			continue
		}
		results = append(results, candidate)
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: results})
}

func sanitizeWeatherConfigCity(city config.WeatherCity) config.WeatherCity {
	return config.WeatherCity{
		Name:        strings.TrimSpace(city.Name),
		Affiliation: strings.TrimSpace(city.Affiliation),
		LocationKey: strings.TrimSpace(city.LocationKey),
		Latitude:    strings.TrimSpace(city.Latitude),
		Longitude:   strings.TrimSpace(city.Longitude),
	}
}

func validateWeatherConfigCity(city config.WeatherCity) error {
	if city.Name == "" {
		return fmt.Errorf("weather_default_city.name is required")
	}
	if city.LocationKey == "" {
		return fmt.Errorf("weather_default_city.location_key is required")
	}
	if city.Latitude == "" {
		return fmt.Errorf("weather_default_city.latitude is required")
	}
	if city.Longitude == "" {
		return fmt.Errorf("weather_default_city.longitude is required")
	}
	return nil
}

func (e *chatToolExecutor) handleGetWeather(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	if e.weatherService == nil {
		return chatToolResult{OK: false, Data: nil, Error: "weather service not configured"}
	}
	if e.configManager == nil {
		return chatToolResult{OK: false, Data: nil, Error: "config manager not configured"}
	}

	var args chatToolGetWeatherArgs
	if errUnmarshal := decodeToolArguments(toolCall.Function.Arguments, &args); errUnmarshal != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}

	queryCity := strings.TrimSpace(args.City)
	resolvedCity := e.configManager.GetWeatherDefaultCity()
	sourceMode := "default"
	if queryCity != "" {
		sourceMode = "explicit"
		searchResults, errSearch := e.weatherService.SearchCities(ctx, queryCity)
		if errSearch != nil {
			return chatToolResult{OK: false, Data: nil, Error: fmt.Sprintf("search city failed: %v", errSearch)}
		}
		if len(searchResults) == 0 {
			return chatToolResult{OK: false, Data: nil, Error: "city not found"}
		}

		locationKey := strings.TrimSpace(searchResults[0].LocationKey)
		if locationKey == "" {
			return chatToolResult{OK: false, Data: nil, Error: "city search result missing location key"}
		}

		cityInfo, errInfo := e.weatherService.GetCityInfo(ctx, locationKey)
		if errInfo != nil {
			return chatToolResult{OK: false, Data: nil, Error: fmt.Sprintf("get city info failed: %v", errInfo)}
		}
		if cityInfo == nil {
			return chatToolResult{OK: false, Data: nil, Error: "city info not found"}
		}

		city := sanitizeWeatherConfigCity(config.WeatherCity{
			Name:        cityInfo.Name,
			Affiliation: cityInfo.Affiliation,
			LocationKey: cityInfo.LocationKey,
			Latitude:    cityInfo.Latitude,
			Longitude:   cityInfo.Longitude,
		})
		if errValidate := validateWeatherConfigCity(city); errValidate != nil {
			return chatToolResult{OK: false, Data: nil, Error: errValidate.Error()}
		}
		resolvedCity = &city
	}

	if resolvedCity == nil {
		return chatToolResult{OK: false, Data: nil, Error: "default weather city is not configured"}
	}

	weatherInfo, errWeather := e.weatherService.GetWeather(
		ctx,
		resolvedCity.LocationKey,
		resolvedCity.Latitude,
		resolvedCity.Longitude,
	)
	if errWeather != nil {
		return chatToolResult{OK: false, Data: nil, Error: fmt.Sprintf("get weather failed: %v", errWeather)}
	}
	if weatherInfo == nil {
		return chatToolResult{OK: false, Data: nil, Error: "weather data not found"}
	}

	return chatToolResult{
		OK:   true,
		Data: buildWeatherToolData(sourceMode, queryCity, resolvedCity, weatherInfo),
	}
}

func buildWeatherToolData(sourceMode, queryCity string, city *config.WeatherCity, weatherInfo *client.WeatherInfo) weatherToolData {
	location := strings.TrimSpace(city.Affiliation)
	if location == "" {
		location = strings.TrimSpace(city.Name)
	}

	data := weatherToolData{
		Location:   location,
		SourceMode: sourceMode,
		UpdatedAt:  formatWeatherUpdateTime(weatherInfo.UpdateTime),
		Current:    buildWeatherCurrentData(weatherInfo.Current),
		AirQuality: buildWeatherAirQualitySummary(weatherInfo.AQI.AQI),
		Alerts:     buildWeatherAlertSummaries(weatherInfo.Alerts),
		Today:      buildWeatherTodaySummary(weatherInfo.ForecastDaily),
		Hourly:     buildWeatherHourlySummaries(weatherInfo.ForecastHourly),
		Rain:       buildWeatherRainSummary(weatherInfo.Minutely),
	}
	if sourceMode == "explicit" {
		data.Query = queryCity
	}
	return data
}

func translateWeatherCode(code string) string {
	code = strings.TrimSpace(code)
	if text, ok := weatherCodeTextMap[code]; ok {
		return text
	}
	return "未知"
}

func buildWeatherCurrentData(current client.CurrentWeather) weatherCurrentData {
	return weatherCurrentData{
		Text:        translateWeatherCode(current.Weather),
		Temperature: formatWeatherTemperature(current.Temperature.Value, current.Temperature.Unit),
		FeelsLike:   formatWeatherTemperature(current.FeelsLike.Value, current.FeelsLike.Unit),
		PublishedAt: strings.TrimSpace(current.PubTime),
	}
}

func formatWeatherTemperature(value, unit string) string {
	combined := strings.TrimSpace(value) + strings.TrimSpace(unit)
	return strings.TrimSpace(combined)
}

func buildWeatherAirQualitySummary(raw string) *weatherAirQualityData {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	value, errParse := strconv.Atoi(raw)
	if errParse != nil {
		return &weatherAirQualityData{Value: raw, Label: "未知"}
	}

	level := 6
	switch {
	case value <= 50:
		level = 1
	case value <= 100:
		level = 2
	case value <= 150:
		level = 3
	case value <= 200:
		level = 4
	case value <= 300:
		level = 5
	}

	label := "严重污染"
	switch level {
	case 1:
		label = "优"
	case 2:
		label = "良"
	case 3:
		label = "轻度污染"
	case 4:
		label = "中度污染"
	case 5:
		label = "重度污染"
	}

	return &weatherAirQualityData{Value: raw, Label: label}
}

func buildWeatherAlertSummaries(alerts []client.WeatherAlert) []weatherAlertData {
	if len(alerts) == 0 {
		return nil
	}

	limit := len(alerts)
	if limit > weatherMaxAlerts {
		limit = weatherMaxAlerts
	}

	results := make([]weatherAlertData, 0, limit)
	for index := 0; index < limit; index++ {
		alert := alerts[index]
		results = append(results, weatherAlertData{
			Title:       strings.TrimSpace(alert.Title),
			Level:       strings.TrimSpace(alert.Level),
			PublishedAt: strings.TrimSpace(alert.PubTime),
		})
	}
	return results
}

func buildWeatherTodaySummary(forecast client.ForecastDaily) *weatherTodayData {
	var summary weatherTodayData
	hasValue := false

	if len(forecast.PrecipitationProbability.Value) > 0 {
		summary.PrecipitationProbability = strings.TrimSpace(forecast.PrecipitationProbability.Value[0])
		hasValue = hasValue || summary.PrecipitationProbability != ""
	}
	if len(forecast.Temperature.Value) > 0 {
		summary.TempLow = formatWeatherTemperature(forecast.Temperature.Value[0].From, "°")
		summary.TempHigh = formatWeatherTemperature(forecast.Temperature.Value[0].To, "°")
		hasValue = hasValue || summary.TempLow != "" || summary.TempHigh != ""
	}
	if len(forecast.Weather.Value) > 0 {
		dayCode := strings.TrimSpace(forecast.Weather.Value[0].From)
		nightCode := strings.TrimSpace(forecast.Weather.Value[0].To)
		summary.Day = translateWeatherCode(dayCode)
		summary.Night = translateWeatherCode(nightCode)
		hasValue = true
	}
	if len(forecast.SunRiseSet.Value) > 0 {
		summary.SunRise = strings.TrimSpace(forecast.SunRiseSet.Value[0].From)
		summary.SunSet = strings.TrimSpace(forecast.SunRiseSet.Value[0].To)
		hasValue = true
	}

	if !hasValue {
		return nil
	}

	if summary.Day == "" {
		summary.Day = "未知"
	}
	if summary.Night == "" {
		summary.Night = "未知"
	}
	return &summary
}

func buildWeatherHourlySummaries(forecast client.ForecastHourly) []weatherHourlyData {
	total := len(forecast.Temperature.Value)
	if len(forecast.Weather.Value) > total {
		total = len(forecast.Weather.Value)
	}
	if total == 0 {
		return nil
	}

	if total > weatherMaxHourlyItems {
		total = weatherMaxHourlyItems
	}

	results := make([]weatherHourlyData, 0, total)
	for index := 0; index < total; index++ {
		item := weatherHourlyData{OffsetHour: index, Text: "未知"}
		if index < len(forecast.Temperature.Value) {
			item.Temperature = forecast.Temperature.Value[index]
		}
		if index < len(forecast.Weather.Value) {
			code := strconv.Itoa(forecast.Weather.Value[index])
			item.Text = translateWeatherCode(code)
		}
		results = append(results, item)
	}
	return results
}

func buildWeatherRainSummary(minutely client.Minutely) weatherRainData {
	var summary weatherRainData
	precipitation := minutely.Precipitation.Value
	if len(precipitation) == 0 {
		return summary
	}

	if precipitation[0] > 0 {
		summary.RainingNow = boolPointer(true)
		for index := 1; index < len(precipitation); index++ {
			if precipitation[index] <= 0 {
				summary.RainEndInMinutes = intPointer(index)
				break
			}
		}
		return summary
	}

	summary.RainingNow = boolPointer(false)
	for index, value := range precipitation {
		if value > 0 {
			summary.NextRainInMinutes = intPointer(index)
			for endIndex := index + 1; endIndex < len(precipitation); endIndex++ {
				if precipitation[endIndex] <= 0 {
					summary.RainEndInMinutes = intPointer(endIndex)
					break
				}
			}
			break
		}
	}
	return summary
}

func formatWeatherUpdateTime(timestampMillis int64) string {
	if timestampMillis <= 0 {
		return ""
	}
	return time.UnixMilli(timestampMillis).In(time.Local).Format(time.RFC3339)
}

func intPointer(value int) *int {
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}
