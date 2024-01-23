package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/fitness/v1"
	"log"
	"net/http"
	"os"
	"time"
)

var googleOauthConfig = &oauth2.Config{
	RedirectURL:  "http://localhost:8080/callback",
	ClientID:     os.Getenv("GOOGLE_FIT_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_FIT_CLIENT_SECRET"),
	Scopes:       []string{"https://www.googleapis.com/auth/fitness.activity.read"},
	Endpoint:     google.Endpoint,
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

	session.Values["google_access_token"] = token.AccessToken
	err = session.Save(c.Request, c.Writer)
	if err != nil {
		log.Printf("Error saving session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, "/")
}

func fetchSteps(fitService *fitness.Service, startTime, endTime int64) (int, error) {
	log.Printf("fetchStepsFromFit called with startTime: %d, endTime: %d\n", startTime, endTime)
	aggRequest := &fitness.AggregateRequest{
		AggregateBy: []*fitness.AggregateBy{
			{
				DataTypeName: "com.google.step_count.delta",
				DataSourceId: "derived:com.google.step_count.delta:com.google.android.gms:estimated_steps",
			},
		},
		BucketByTime: &fitness.BucketByTime{
			DurationMillis: 86400000,
		},
		StartTimeMillis: startTime,
		EndTimeMillis:   endTime,
	}

	dataset, err := fitService.Users.Dataset.Aggregate("me", aggRequest).Do()
	if err != nil {
		log.Printf("Error making aggregate request to Google Fit API: %v\n", err)
		return 0, err
	}

	log.Printf("Raw dataset response from Google Fit: %+v\n", dataset)

	totalSteps := extractSteps(dataset)
	log.Printf("Total steps extracted: %d\n", totalSteps)

	return totalSteps, nil
}

func fetchTotalSteps(fitService *fitness.Service, dateType string, date time.Time) (int, error) {
	var totalSteps int
	var err error

	switch dateType {
	case "year":
		for month := 1; month <= 12; month++ {
			startOfMonth := time.Date(date.Year(), time.Month(month), 1, 0, 0, 0, 0, date.Location())
			endOfMonth := startOfMonth.AddDate(0, 1, 0).Add(-time.Millisecond)
			startMillis := startOfMonth.UnixNano() / int64(time.Millisecond)
			endMillis := endOfMonth.UnixNano() / int64(time.Millisecond)
			log.Printf("Fetching steps for month: %d, year: %d\n", month, date.Year())
			monthlySteps, err := fetchSteps(fitService, startMillis, endMillis)
			if err != nil {
				log.Printf("Error fetching steps for month %d: %s\n", month, err)
				continue
			}
			log.Printf("Steps fetched for month %d: %d\n", month, monthlySteps)
			totalSteps += monthlySteps
		}

	case "month", "day":
		startTime, endTime := getRange(date, dateType)
		totalSteps, err = fetchSteps(fitService, startTime, endTime)
		if err != nil {
			return 0, fmt.Errorf("error fetching steps for %s: %v", dateType, err)
		}
	}
	return totalSteps, nil
}

func extractSteps(response *fitness.AggregateResponse) int {
	var totalSteps int
	for _, bucket := range response.Bucket {
		for _, dataSet := range bucket.Dataset {
			for _, dataPoint := range dataSet.Point {
				for _, val := range dataPoint.Value {
					totalSteps += int(val.IntVal)
				}
			}
		}
	}
	return totalSteps
}
