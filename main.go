package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
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
)

func main() {
	r := gin.Default()
	r.GET("/", handleMain)
	r.GET("/login", handleGoogleLogin)
	r.GET("/callback", handleGoogleCallback)
	r.GET("/day/:date", handleGetData)

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
	accessToken := c.Query("access_token")

	// Check if the access token is present
	if accessToken == "" {
		// Redirect to login if no access token is found
		// Pass the requested date as a state parameter
		requestedDate := c.Param("date")
		loginURL := fmt.Sprintf("/login?state=%s", requestedDate)
		c.Redirect(http.StatusTemporaryRedirect, loginURL)
		return
	}

	date := c.Param("date")
	startTime, endTime, err := getDateRange(date)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	// Log the start and end time for debugging
	log.Printf("Date: %s, Start Time: %d, End Time: %d\n", date, startTime, endTime)

	tokenSource := googleOauthConfig.TokenSource(context.Background(), &oauth2.Token{
		AccessToken: accessToken,
	})
	httpClient := oauth2.NewClient(context.Background(), tokenSource)

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
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Log the request body for debugging
	log.Printf("Request Body: %s\n", string(requestBody))

	req, err := http.NewRequest("POST", "https://www.googleapis.com/fitness/v1/users/me/dataset:aggregate", bytes.NewBuffer(requestBody))
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	req.Header.Set("Content-Type", "application/json; encoding=utf-8")

	resp, err := httpClient.Do(req)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer resp.Body.Close()

	// Read the response for debugging
	respBody, _ := io.ReadAll(resp.Body)
	log.Println("Response from Google Fit API: ", string(respBody))

	// Reset the response body for decoding
	resp.Body = io.NopCloser(bytes.NewBuffer(respBody))

	var responseData GoogleFitAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	steps := extractSteps(responseData)
	c.JSON(http.StatusOK, gin.H{"date": date, "steps": steps})
}

func getDateRange(dateStr string) (int64, int64, error) {
	layout := "2006-01-02"
	loc, err := time.LoadLocation("America/Chicago") // Central Time Zone (Oklahoma)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to load location: %v", err)
	}

	parsedDate, err := time.ParseInLocation(layout, dateStr, loc)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid date format: %v", err)
	}

	startOfDay := time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 0, 0, 0, 0, loc)
	endOfDay := startOfDay.AddDate(0, 0, 1).Add(-time.Millisecond)

	startMillis := startOfDay.UnixNano() / int64(time.Millisecond)
	endMillis := endOfDay.UnixNano() / int64(time.Millisecond)

	return startMillis, endMillis, nil
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
