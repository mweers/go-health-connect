package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"log"
	"net/http"
	"strconv"
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
)

func main() {
	r := gin.Default()
	r.GET("/", handleMain)
	r.GET("/login", handleGoogleLogin)
	r.GET("/callback", handleGoogleCallback)
	r.GET("/data/:date", handleGetData)

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
	state := c.Query("state") // This will be the originally requested date

	token, err := googleOauthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Could not get access token: %s\n", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Redirect to the /day endpoint with the original date and access token
	c.Redirect(http.StatusFound, fmt.Sprintf("/day/%s?access_token=%s", state, token.AccessToken))
}

func handleGetData(c *gin.Context) {
	dateStr := c.Param("date")

	layout, rangeType := getDateFormat(dateStr)
	if layout == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format"})
		return
	}

	accessToken := c.Query("access_token")
	if accessToken == "" {
		loginURL := fmt.Sprintf("/login?state=%s", dateStr)
		c.Redirect(http.StatusTemporaryRedirect, loginURL)
		return
	}

	tokenSource := googleOauthConfig.TokenSource(context.Background(), &oauth2.Token{AccessToken: accessToken})
	httpClient := oauth2.NewClient(context.Background(), tokenSource)

	var totalSteps int
	var err error

	switch rangeType {
	case "year":
		// Special handling for year-long data
		year, _ := strconv.Atoi(dateStr)
		loc, _ := time.LoadLocation("America/Chicago") // Adjust timezone as needed
		for month := 1; month <= 12; month++ {
			startOfMonth := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, loc)
			endOfMonth := startOfMonth.AddDate(0, 1, 0).Add(-time.Millisecond)
			startMillis := startOfMonth.UnixNano() / int64(time.Millisecond)
			endMillis := endOfMonth.UnixNano() / int64(time.Millisecond)
			monthlySteps, err := fetchSteps(httpClient, startMillis, endMillis)
			if err != nil {
				log.Printf("Error fetching steps for month %d: %s\n", month, err)
				continue // Skip this month if there's an error
			}
			totalSteps += monthlySteps
		}
	case "day", "month":
		parsedDate, _ := time.Parse(layout, dateStr)
		startTime, endTime := getRange(parsedDate, rangeType)
		totalSteps, err = fetchSteps(httpClient, startTime, endTime)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching steps"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid range type"})
		return
	}
	c.JSON(http.StatusOK, gin.H{rangeType: dateStr, "steps": totalSteps})
}

func getRange(date time.Time, rangeType string) (int64, int64) {
	switch rangeType {
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

func fetchSteps(client *http.Client, startTime, endTime int64) (int, error) {
	requestBody, err := json.Marshal(map[string]interface{}{
		"aggregateBy": []map[string]string{
			{
				"dataTypeName": "com.google.step_count.delta",
				"dataSourceId": "derived:com.google.step_count.delta:com.google.android.gms:estimated_steps",
			},
		},
		"bucketByTime": map[string]int64{
			"durationMillis": 86400000, // Aggregate by day
		},
		"startTimeMillis": startTime,
		"endTimeMillis":   endTime,
	})
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("POST", "https://www.googleapis.com/fitness/v1/users/me/dataset:aggregate", bytes.NewBuffer(requestBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json; encoding=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var responseData GoogleFitAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return 0, err
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

func getDateFormat(dateStr string) (string, string) {
	if len(dateStr) == 4 {
		return "2006", "year"
	} else if len(dateStr) == 7 {
		return "2006-01", "month"
	} else if len(dateStr) == 10 {
		return "2006-01-02", "day"
	} else {
		return "", ""
	}
}
