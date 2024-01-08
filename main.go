package main

import (
	"bytes"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
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
	r.GET("/data/:date", handleGetData)

	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func handleMain(c *gin.Context) {
	c.String(http.StatusOK, "Welcome to the Google Fit API example!")
}

func handleGoogleLogin(c *gin.Context) {
	url := googleOauthConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func handleGoogleCallback(c *gin.Context) {
	code := c.Query("code")
	token, err := googleOauthConfig.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Printf("Could not get access token: %s\n", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, "/data/your-date-here?access_token="+token.AccessToken)
}

func handleGetData(c *gin.Context) {
	date := c.Param("date")
	accessToken := c.Query("access_token")

	startTime, endTime, err := getDateRange(date)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	tokenSource := googleOauthConfig.TokenSource(oauth2.NoContext, &oauth2.Token{
		AccessToken: accessToken,
	})
	httpClient := oauth2.NewClient(oauth2.NoContext, tokenSource)

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
	parsedDate, err := time.Parse(layout, dateStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid date format: %v", err)
	}
	startOfDay := time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 0, 0, 0, 0, parsedDate.Location())
	endOfDay := startOfDay.AddDate(0, 0, 1).Add(-time.Millisecond)

	startMillis := startOfDay.UnixNano() / int64(time.Millisecond)
	endMillis := endOfDay.UnixNano() / int64(time.Millisecond)

	return startMillis, endMillis, nil
}

type GoogleFitAPIResponse struct {
	MinStartTimeNs string `json:"minStartTimeNs"`
	MaxEndTimeNs   string `json:"maxEndTimeNs"`
	DataSourceId   string `json:"dataSourceId"`
	Point          []struct {
		ModifiedTimeMillis string `json:"modifiedTimeMillis"`
		StartTimeNanos     string `json:"startTimeNanos"`
		EndTimeNanos       string `json:"endTimeNanos"`
		Value              []struct {
			IntVal int `json:"intVal"`
		} `json:"value"`
		DataTypeName       string `json:"dataTypeName"`
		OriginDataSourceId string `json:"originDataSourceId"`
	} `json:"point"`
}

func extractSteps(data GoogleFitAPIResponse) int {
	var totalSteps int
	for _, point := range data.Point {
		for _, value := range point.Value {
			totalSteps += value.IntVal
		}
	}
	return totalSteps
}
