package main

import (
	"context"
	"fmt"
	ginsessions "github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"google.golang.org/api/fitness/v1"
	"google.golang.org/api/option"
	"log"
	"net/http"
	"time"
)

var (
	store ginsessions.CookieStore
)

func init() {
	secretKey := []byte("FqUhFPtOwOIcA4X3VxtHt1ievZVXNLVQ")
	store = ginsessions.NewCookieStore(secretKey)
}

func main() {
	r := gin.Default()
	r.Use(ginsessions.Sessions("session-name", store))
	r.GET("/", handleMain)
	r.GET("/login", handleGoogleLogin)
	r.GET("/callback", handleGoogleCallback)
	r.GET("/loginWithings", handleWithingsLogin)
	r.GET("/callbackWithings", handleWithingsCallback)
	r.GET("/:date", handleData)

	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func handleMain(c *gin.Context) {
	c.String(http.StatusOK, "Welcome to the Google Fit API example!")
}

func handleData(c *gin.Context) {
	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		log.Printf("Error getting session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	googleAccessToken, ok := session.Values["google_access_token"].(string)
	if !ok || googleAccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Google Access token not found. Please log in again."})
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

	ctx := context.Background()
	googleToken := &oauth2.Token{AccessToken: googleAccessToken}
	fitService, err := fitness.NewService(ctx, option.WithTokenSource(googleOauthConfig.TokenSource(ctx, googleToken)))
	if err != nil {
		log.Printf("Error creating Google Fit client: %s", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating Google Fit client"})
		return
	}

	totalSteps, err := fetchTotalSteps(fitService, dateType, parsedDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching steps"})
		return
	}

	log.Println("Fetching Withings data...")

	session, err = store.Get(c.Request, "session-name")
	if err != nil {
		log.Printf("Error getting session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	response := gin.H{
		dateType: dateStr,
		"steps":  totalSteps,
	}

	if dateType == "day" {
		weightData, err := fetchWithingsData(c, parsedDate)
		if err != nil {
			log.Printf("Error fetching weight data from Withings: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching weight data"})
			return
		}

		if len(weightData.Body.MeasureGrps) > 0 && len(weightData.Body.MeasureGrps[0].Measures) > 0 {
			weightInPounds := convertToPounds(weightData.Body.MeasureGrps[0].Measures[0].Value, weightData.Body.MeasureGrps[0].Measures[0].Unit)
			log.Printf("Your weight is: %.2f lbs\n", weightInPounds)
			response["weight"] = weightInPounds
		} else {
			log.Println("No weight data available for the specified day.")
			response["weight"] = "No data"
		}
	} else {
		log.Println("Data request is not for a single day. Weight data will not be included.")
	}

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
