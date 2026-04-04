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

type weatherService interface {
	SearchCities(ctx context.Context, keyword string) ([]client.WeatherCity, error)
	GetCityInfo(ctx context.Context, locationKey string) (*client.WeatherCity, error)
	GetWeather(ctx context.Context, locationKey, latitude, longitude string) (*client.WeatherInfo, error)
}

type chatToolGetWeatherArgs struct {
	City string `json:"city,omitempty"`
}

type weatherToolSummary struct {
	City        string                   `json:"city"`
	Affiliation string                   `json:"affiliation,omitempty"`
	SourceCity  weatherToolSourceCity    `json:"source_city"`
	UpdatedAt   string                   `json:"updated_at,omitempty"`
	Current     weatherCurrentSummary    `json:"current"`
	AirQuality  weatherAirQualitySummary `json:"air_quality"`
	Alerts      []weatherAlertSummary    `json:"alerts"`
	Today       *weatherTodaySummary     `json:"today,omitempty"`
	Hourly      []weatherHourlySummary   `json:"hourly"`
	Minutely    weatherMinutelySummary   `json:"minutely"`
}

type weatherToolSourceCity struct {
	Mode        string `json:"mode"`
	Query       string `json:"query,omitempty"`
	Name        string `json:"name"`
	Affiliation string `json:"affiliation,omitempty"`
	LocationKey string `json:"location_key"`
}

type weatherMetricSummary struct {
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type weatherWindSummary struct {
	Direction weatherMetricSummary `json:"direction"`
	Speed     weatherMetricSummary `json:"speed"`
}

type weatherCurrentSummary struct {
	WeatherCode string               `json:"weather_code"`
	WeatherText string               `json:"weather_text"`
	Temperature weatherMetricSummary `json:"temperature"`
	FeelsLike   weatherMetricSummary `json:"feels_like"`
	Humidity    weatherMetricSummary `json:"humidity"`
	Pressure    weatherMetricSummary `json:"pressure"`
	Visibility  weatherMetricSummary `json:"visibility"`
	Wind        weatherWindSummary   `json:"wind"`
	PublishedAt string               `json:"published_at,omitempty"`
}

type weatherAirQualitySummary struct {
	Value string `json:"value"`
	Level int    `json:"level"`
	Label string `json:"label"`
}

type weatherAlertSummary struct {
	Title       string `json:"title"`
	Type        string `json:"type,omitempty"`
	Level       string `json:"level,omitempty"`
	Detail      string `json:"detail,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type weatherTodayTemperatureSummary struct {
	Low  string `json:"low,omitempty"`
	High string `json:"high,omitempty"`
	Unit string `json:"unit,omitempty"`
}

type weatherTodayCodeSummary struct {
	DayCode   string `json:"day_code,omitempty"`
	DayText   string `json:"day_text,omitempty"`
	NightCode string `json:"night_code,omitempty"`
	NightText string `json:"night_text,omitempty"`
}

type weatherTodaySummary struct {
	PrecipitationProbability string                         `json:"precipitation_probability,omitempty"`
	Temperature              weatherTodayTemperatureSummary `json:"temperature"`
	Weather                  weatherTodayCodeSummary        `json:"weather"`
	SunRise                  string                         `json:"sun_rise,omitempty"`
	SunSet                   string                         `json:"sun_set,omitempty"`
}

type weatherHourlySummary struct {
	OffsetHour  int     `json:"offset_hour"`
	Temperature float64 `json:"temperature"`
	WeatherCode string  `json:"weather_code,omitempty"`
	WeatherText string  `json:"weather_text"`
}

type weatherMinutelySummary struct {
	Precipitation     []float64 `json:"precipitation"`
	NextRainInMinutes *int      `json:"next_rain_in_minutes,omitempty"`
	RainEndInMinutes  *int      `json:"rain_end_in_minutes,omitempty"`
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
		Data: buildWeatherToolSummary(sourceMode, queryCity, resolvedCity, weatherInfo),
	}
}

func buildWeatherToolSummary(sourceMode, queryCity string, city *config.WeatherCity, weatherInfo *client.WeatherInfo) weatherToolSummary {
	return weatherToolSummary{
		City:        city.Name,
		Affiliation: city.Affiliation,
		SourceCity: weatherToolSourceCity{
			Mode:        sourceMode,
			Query:       queryCity,
			Name:        city.Name,
			Affiliation: city.Affiliation,
			LocationKey: city.LocationKey,
		},
		UpdatedAt:  formatWeatherUpdateTime(weatherInfo.UpdateTime),
		Current:    buildWeatherCurrentSummary(weatherInfo.Current),
		AirQuality: buildWeatherAirQualitySummary(weatherInfo.AQI.AQI),
		Alerts:     buildWeatherAlertSummaries(weatherInfo.Alerts),
		Today:      buildWeatherTodaySummary(weatherInfo.ForecastDaily),
		Hourly:     buildWeatherHourlySummaries(weatherInfo.ForecastHourly),
		Minutely:   buildWeatherMinutelySummary(weatherInfo.Minutely),
	}
}

func translateWeatherCode(code string) string {
	code = strings.TrimSpace(code)
	if text, ok := weatherCodeTextMap[code]; ok {
		return text
	}
	return "未知"
}

func buildWeatherCurrentSummary(current client.CurrentWeather) weatherCurrentSummary {
	return weatherCurrentSummary{
		WeatherCode: strings.TrimSpace(current.Weather),
		WeatherText: translateWeatherCode(current.Weather),
		Temperature: buildWeatherMetricSummary(current.Temperature),
		FeelsLike:   buildWeatherMetricSummary(current.FeelsLike),
		Humidity:    buildWeatherMetricSummary(current.Humidity),
		Pressure:    buildWeatherMetricSummary(current.Pressure),
		Visibility:  buildWeatherMetricSummary(current.Visibility),
		Wind: weatherWindSummary{
			Direction: buildWeatherMetricSummary(current.Wind.Direction),
			Speed:     buildWeatherMetricSummary(current.Wind.Speed),
		},
		PublishedAt: strings.TrimSpace(current.PubTime),
	}
}

func buildWeatherMetricSummary(metric client.WeatherMetric) weatherMetricSummary {
	return weatherMetricSummary{
		Value: strings.TrimSpace(metric.Value),
		Unit:  strings.TrimSpace(metric.Unit),
	}
}

func buildWeatherAirQualitySummary(raw string) weatherAirQualitySummary {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return weatherAirQualitySummary{Value: "", Level: 0, Label: "未知"}
	}

	value, errParse := strconv.Atoi(raw)
	if errParse != nil {
		return weatherAirQualitySummary{Value: raw, Level: 0, Label: "未知"}
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

	return weatherAirQualitySummary{
		Value: raw,
		Level: level,
		Label: label,
	}
}

func buildWeatherAlertSummaries(alerts []client.WeatherAlert) []weatherAlertSummary {
	if len(alerts) == 0 {
		return []weatherAlertSummary{}
	}

	results := make([]weatherAlertSummary, 0, len(alerts))
	for _, alert := range alerts {
		results = append(results, weatherAlertSummary{
			Title:       strings.TrimSpace(alert.Title),
			Type:        strings.TrimSpace(alert.Type),
			Level:       strings.TrimSpace(alert.Level),
			Detail:      strings.TrimSpace(alert.Detail),
			PublishedAt: strings.TrimSpace(alert.PubTime),
		})
	}
	return results
}

func buildWeatherTodaySummary(forecast client.ForecastDaily) *weatherTodaySummary {
	var summary weatherTodaySummary
	hasValue := false

	if len(forecast.PrecipitationProbability.Value) > 0 {
		summary.PrecipitationProbability = strings.TrimSpace(forecast.PrecipitationProbability.Value[0])
		hasValue = hasValue || summary.PrecipitationProbability != ""
	}
	if len(forecast.Temperature.Value) > 0 {
		summary.Temperature = weatherTodayTemperatureSummary{
			Low:  strings.TrimSpace(forecast.Temperature.Value[0].From),
			High: strings.TrimSpace(forecast.Temperature.Value[0].To),
			Unit: "°",
		}
		hasValue = true
	}
	if len(forecast.Weather.Value) > 0 {
		dayCode := strings.TrimSpace(forecast.Weather.Value[0].From)
		nightCode := strings.TrimSpace(forecast.Weather.Value[0].To)
		summary.Weather = weatherTodayCodeSummary{
			DayCode:   dayCode,
			DayText:   translateWeatherCode(dayCode),
			NightCode: nightCode,
			NightText: translateWeatherCode(nightCode),
		}
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
	return &summary
}

func buildWeatherHourlySummaries(forecast client.ForecastHourly) []weatherHourlySummary {
	total := len(forecast.Temperature.Value)
	if len(forecast.Weather.Value) > total {
		total = len(forecast.Weather.Value)
	}
	if total == 0 {
		return []weatherHourlySummary{}
	}

	results := make([]weatherHourlySummary, 0, total)
	for index := 0; index < total; index++ {
		item := weatherHourlySummary{OffsetHour: index}
		if index < len(forecast.Temperature.Value) {
			item.Temperature = forecast.Temperature.Value[index]
		}
		if index < len(forecast.Weather.Value) {
			code := strconv.Itoa(forecast.Weather.Value[index])
			item.WeatherCode = code
			item.WeatherText = translateWeatherCode(code)
		} else {
			item.WeatherText = "未知"
		}
		results = append(results, item)
	}
	return results
}

func buildWeatherMinutelySummary(minutely client.Minutely) weatherMinutelySummary {
	summary := weatherMinutelySummary{
		Precipitation: minutely.Precipitation.Value,
	}
	if len(summary.Precipitation) == 0 {
		return summary
	}

	if summary.Precipitation[0] > 0 {
		for index := 1; index < len(summary.Precipitation); index++ {
			if summary.Precipitation[index] <= 0 {
				summary.RainEndInMinutes = intPointer(index)
				break
			}
		}
		return summary
	}

	for index, value := range summary.Precipitation {
		if value > 0 {
			summary.NextRainInMinutes = intPointer(index)
			for endIndex := index + 1; endIndex < len(summary.Precipitation); endIndex++ {
				if summary.Precipitation[endIndex] <= 0 {
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
