package client

import (
	"context"
	"cornerstone/logging"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWeatherBaseURL = "https://weatherapi.market.xiaomi.com"
	weatherRequestTimeout = 20 * time.Second
	weatherLocale         = "zh_cn"
	weatherAppKey         = "weather20151024"
	weatherSign           = "zUFJoAR2ZVrDy1vF3D07"
	weatherForecastDays   = 15
)

type WeatherClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

type WeatherCity struct {
	Name        string `json:"name"`
	Affiliation string `json:"affiliation"`
	LocationKey string `json:"locationKey"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}

type WeatherMetric struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

type WeatherWind struct {
	Direction WeatherMetric `json:"direction"`
	Speed     WeatherMetric `json:"speed"`
}

type CurrentWeather struct {
	FeelsLike   WeatherMetric `json:"feelsLike"`
	Humidity    WeatherMetric `json:"humidity"`
	Pressure    WeatherMetric `json:"pressure"`
	Temperature WeatherMetric `json:"temperature"`
	Visibility  WeatherMetric `json:"visibility"`
	Weather     string        `json:"weather"`
	PubTime     string        `json:"pubTime"`
	Wind        WeatherWind   `json:"wind"`
}

type WeatherAlertImages struct {
	Icon string `json:"icon"`
}

type WeatherAlert struct {
	LocationKey string             `json:"locationKey"`
	AlertID     string             `json:"alertId"`
	PubTime     string             `json:"pubTime"`
	Title       string             `json:"title"`
	Type        string             `json:"type"`
	Level       string             `json:"level"`
	Detail      string             `json:"detail"`
	Images      WeatherAlertImages `json:"images"`
}

type StringArrayValue struct {
	Value []string `json:"value"`
}

type FloatArrayValue struct {
	Value []float64 `json:"value"`
}

type IntArrayValue struct {
	Value []int `json:"value"`
}

type DailyTemperatureRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type DailyWeatherRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type SunRiseSetRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type DailyTemperatureValue struct {
	Value []DailyTemperatureRange `json:"value"`
}

type DailyWeatherValue struct {
	Value []DailyWeatherRange `json:"value"`
}

type SunRiseSetValue struct {
	Value []SunRiseSetRange `json:"value"`
}

type ForecastDaily struct {
	PrecipitationProbability StringArrayValue      `json:"precipitationProbability"`
	Temperature              DailyTemperatureValue `json:"temperature"`
	Weather                  DailyWeatherValue     `json:"weather"`
	SunRiseSet               SunRiseSetValue       `json:"sunRiseSet"`
}

type ForecastHourly struct {
	Temperature FloatArrayValue `json:"temperature"`
	Weather     IntArrayValue   `json:"weather"`
}

type Minutely struct {
	Precipitation FloatArrayValue `json:"precipitation"`
}

type AQI struct {
	AQI string `json:"aqi"`
}

type WeatherInfo struct {
	Current        CurrentWeather `json:"current"`
	Alerts         []WeatherAlert `json:"alerts"`
	UpdateTime     int64          `json:"updateTime"`
	ForecastDaily  ForecastDaily  `json:"forecastDaily"`
	ForecastHourly ForecastHourly `json:"forecastHourly"`
	Minutely       Minutely       `json:"minutely"`
	AQI            AQI            `json:"aqi"`
}

func NewWeatherClient(baseURL string) *WeatherClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultWeatherBaseURL
	}
	return &WeatherClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: newHTTPClient(),
	}
}

func (c *WeatherClient) SearchCities(ctx context.Context, keyword string) ([]WeatherCity, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("city keyword is required")
	}

	var cities []WeatherCity
	if err := c.doGetJSON(ctx, "/wtr-v3/location/city/search", url.Values{
		"name":   []string{keyword},
		"locale": []string{weatherLocale},
	}, &cities); err != nil {
		return nil, err
	}
	return cities, nil
}

func (c *WeatherClient) GetCityInfo(ctx context.Context, locationKey string) (*WeatherCity, error) {
	locationKey = strings.TrimSpace(locationKey)
	if locationKey == "" {
		return nil, fmt.Errorf("location key is required")
	}

	var cities []WeatherCity
	if err := c.doGetJSON(ctx, "/wtr-v3/location/city/info", url.Values{
		"locationKey": []string{locationKey},
		"locale":      []string{weatherLocale},
	}, &cities); err != nil {
		return nil, err
	}
	if len(cities) == 0 {
		return nil, fmt.Errorf("city info not found")
	}
	return &cities[0], nil
}

func (c *WeatherClient) GetWeather(ctx context.Context, locationKey, latitude, longitude string) (*WeatherInfo, error) {
	locationKey = strings.TrimSpace(locationKey)
	latitude = strings.TrimSpace(latitude)
	longitude = strings.TrimSpace(longitude)
	if locationKey == "" || latitude == "" || longitude == "" {
		return nil, fmt.Errorf("location key and coordinates are required")
	}

	var info WeatherInfo
	if err := c.doGetJSON(ctx, "/wtr-v3/weather/all", url.Values{
		"latitude":    []string{latitude},
		"longitude":   []string{longitude},
		"locationKey": []string{locationKey},
		"days":        []string{strconv.Itoa(weatherForecastDays)},
		"appKey":      []string{weatherAppKey},
		"sign":        []string{weatherSign},
		"isGlobal":    []string{"false"},
		"locale":      []string{weatherLocale},
	}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *WeatherClient) doGetJSON(ctx context.Context, path string, query url.Values, dst interface{}) error {
	ctx, cancel := context.WithTimeout(ctx, weatherRequestTimeout)
	defer cancel()

	endpoint := c.BaseURL + path
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, errCreate := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if errCreate != nil {
		return fmt.Errorf("create request: %w", errCreate)
	}

	resp, errDo := c.HTTPClient.Do(req)
	if errDo != nil {
		return fmt.Errorf("do request: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			logging.Warnf("close weather body error: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("weather API error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if errDecode := json.NewDecoder(resp.Body).Decode(dst); errDecode != nil {
		return fmt.Errorf("decode response: %w", errDecode)
	}
	return nil
}
