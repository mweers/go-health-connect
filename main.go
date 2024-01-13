package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"io"
	"log"
	"net/http"
	"time"
)

var (
	googleOauthConfig = &oauth2.Config{
		RedirectURL:  "http://localhost:8080/callback",
		ClientID:     "404858114083-2rbidruv4j9f5vepkktju9lto6ahpt40.apps.googleusercontent.com",
		ClientSecret: "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",
		Scopes:       []string{"https://www.googleapis.com/auth/fitness.activity.read"},
		Endpoint:     google.Endpoint,
	}
	store *sessions.CookieStore
)

func init() {
	secretKey := []byte("FqUhFPtOwOIcA4X3VxtHt1ievZVXNLVQ")
	store = sessions.NewCookieStore([]byte(secretKey))
}

func main() {
	r := gin.Default()
	r.GET("/", handleMain)
	r.GET("/login", handleGoogleLogin)
	r.GET("/callback", handleGoogleCallback)
	r.GET("/:date", handleData)

	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func handleMain(c *gin.Context) {
	c.String(http.StatusOK, "Welcome to the Google Fit API example!")
}

func handleGoogleLogin(c *gin.Context) {
	state := c.DefaultQuery("state", "")
	url := googleOauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func handleGoogleCallback(c *gin.Context) {
	code := c.Query("code")

	token, err := googleOauthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Could not get access token: %s\n", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		log.Printf("Error getting session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	session.Values["access_token"] = token.AccessToken
	err = session.Save(c.Request, c.Writer)
	if err != nil {
		log.Printf("Error saving session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, "/")
}

func handleData(c *gin.Context) {
	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		log.Printf("Error getting session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	accessToken, ok := session.Values["access_token"].(string)
	if !ok || accessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Access token not found. Please log in again."})
		return
	}

	dateStr := c.Param("date")
	dateType, err := getDateType(dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parsedDate, err := parseDate(dateStr, dateType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format"})
		return
	}

	tokenSource := googleOauthConfig.TokenSource(context.Background(), &oauth2.Token{AccessToken: accessToken})
	httpClient := oauth2.NewClient(context.Background(), tokenSource)

	totalSteps, err := fetchTotalSteps(httpClient, dateType, parsedDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching steps"})
		return
	}

	response := make(map[string]interface{})
	response[dateType] = dateStr
	response["steps"] = totalSteps

	c.JSON(http.StatusOK, response)
}

func getDateType(dateStr string) (string, error) {
	switch len(dateStr) {
	case 4:
		return "year", nil
	case 7:
		return "month", nil
	case 10:
		return "day", nil
	default:
		return "", fmt.Errorf("invalid date length")
	}
}

func parseDate(dateStr, dateType string) (time.Time, error) {
	var layout string
	switch dateType {
	case "year":
		layout = "2006"
	case "month":
		layout = "2006-01"
	case "day":
		layout = "2006-01-02"
	}
	return time.Parse(layout, dateStr)
}

func getRange(date time.Time, dateType string) (int64, int64) {
	switch dateType {
	case "day":
		start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
		end := start.AddDate(0, 0, 1).Add(-time.Millisecond)
		return start.UnixNano() / int64(time.Millisecond), end.UnixNano() / int64(time.Millisecond)
	case "month":
		start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
		end := start.AddDate(0, 1, 0).Add(-time.Millisecond)
		return start.UnixNano() / int64(time.Millisecond), end.UnixNano() / int64(time.Millisecond)
	default:
		return 0, 0
	}
}

func fetchTotalSteps(client *http.Client, dateType string, date time.Time) (int, error) {
	var totalSteps int
	var err error

	switch dateType {
	case "year":
		for month := 1; month <= 12; month++ {
			startOfMonth := time.Date(date.Year(), time.Month(month), 1, 0, 0, 0, 0, date.Location())
			endOfMonth := startOfMonth.AddDate(0, 1, 0).Add(-time.Millisecond)
			startMillis := startOfMonth.UnixNano() / int64(time.Millisecond)
			endMillis := endOfMonth.UnixNano() / int64(time.Millisecond)

			monthlySteps, err := fetchSteps(client, startMillis, endMillis)
			if err != nil {
				log.Printf("Error fetching steps for month %d: %s\n", month, err)
				continue
			}
			totalSteps += monthlySteps
		}

	case "month", "day":
		startTime, endTime := getRange(date, dateType)
		totalSteps, err = fetchSteps(client, startTime, endTime)
		if err != nil {
			return 0, fmt.Errorf("error fetching steps for %s: %v", dateType, err)
		}
	}
	return totalSteps, nil
}

func fetchSteps(client *http.Client, startTime, endTime int64) (int, error) {
	requestBody, err := json.Marshal(map[string]interface{}{
		"aggregateBy": []map[string]string{
			{
				"dataTypeName": "com.google.step_count.delta",
				"dataSourceId": "derived:com.google.step_count.delta:com.google.android.gms:estimated_steps",
			},
		},
		"bucketByTime": map[string]int64{
			"durationMillis": 86400000,
		},
		"startTimeMillis": startTime,
		"endTimeMillis":   endTime,
	})
	if err != nil {
		return 0, fmt.Errorf("error marshaling request body: %v", err)
	}

	req, err := http.NewRequest("POST", "https://www.googleapis.com/fitness/v1/users/me/dataset:aggregate", bytes.NewBuffer(requestBody))
	if err != nil {
		return 0, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json; encoding=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error making API request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
		}
	}(resp.Body)

	var responseData GoogleFitAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return 0, fmt.Errorf("error decoding API response: %v", err)
	}
	return extractSteps(responseData), nil
}

type GoogleFitAPIResponse struct {
	Bucket []struct {
		DataSet []struct {
			Point []struct {
				Value []struct {
					IntVal int `json:"intVal"`
				} `json:"value"`
			} `json:"point"`
		} `json:"dataset"`
	} `json:"bucket"`
}

func extractSteps(data GoogleFitAPIResponse) int {
	var totalSteps int
	for _, bucket := range data.Bucket {
		for _, dataSet := range bucket.DataSet {
			for _, point := range dataSet.Point {
				for _, value := range point.Value {
					totalSteps += value.IntVal
				}
			}
		}
	}
	return totalSteps
}
