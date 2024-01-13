package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/fitness/v1"
	"google.golang.org/api/option"
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

	withingsOauthConfig = &oauth2.Config{
		RedirectURL:  "http://localhost:8080/callback",
		ClientID:     "fdb372f760c5543c30ded5b770af67d904b376559edabe2521bb74e7bd5e02e7",
		ClientSecret: "8498abe0a56dc901ccdb6e5c5dbe5bb80185ec865a74e2718bf547b26f12a9c1",
		Scopes:       []string{"user.activity"}, // Set the required scopes
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://account.withings.com/oauth2_user/authorize2",
			TokenURL: "https://wbsapi.withings.net/v2/oauth2",
		},
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

func redirectToWithingsAuth(w http.ResponseWriter, r *http.Request) {
	url := withingsOauthConfig.AuthCodeURL("state", oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

type WithingsData struct {
	// Define the structure according to the Withings API response
}

func handleWithingsCallback(c *gin.Context) {
	code := c.Query("code")

	token, err := withingsOauthConfig.Exchange(context.Background(), code)
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

func fetchWithingsData(token *oauth2.Token, dataType string) (*WithingsAPIResponse, error) {
	client := withingsOauthConfig.Client(context.Background(), token)

	req, err := http.NewRequest("GET", "https://wbsapi.withings.net/"+dataType, nil)
	if err != nil {
		return nil, err
	}

	// Add any necessary query parameters here
	// ...

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResponse WithingsAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, err
	}

	return &apiResponse, nil
}

type WithingsAPIResponse struct {
	Status int          `json:"status"`
	Body   WithingsBody `json:"body"`
}

type WithingsBody struct {
	UpdateTime  string       `json:"updatetime"`
	Timezone    string       `json:"timezone"`
	MeasureGrps []MeasureGrp `json:"measuregrps"`
	More        int          `json:"more"`
	Offset      int          `json:"offset"`
}

type MeasureGrp struct {
	GrpID    int       `json:"grpid"`
	Attrib   int       `json:"attrib"`
	Date     int64     `json:"date"`
	Created  int64     `json:"created"`
	Modified int64     `json:"modified"`
	Category int64     `json:"category"`
	DeviceID string    `json:"deviceid"`
	Measures []Measure `json:"measures"`
	Comment  string    `json:"comment"`
	Timezone string    `json:"timezone"`
}

type Measure struct {
	Value    int `json:"value"`
	Type     int `json:"type"`
	Unit     int `json:"unit"`
	Algo     int `json:"algo"`
	FM       int `json:"fm"`
	Position int `json:"position"`
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

	ctx := context.Background()
	token := &oauth2.Token{AccessToken: accessToken}
	fitService, err := fitness.NewService(ctx, option.WithTokenSource(googleOauthConfig.TokenSource(ctx, token)))
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
