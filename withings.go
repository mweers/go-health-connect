package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"golang.org/x/oauth2"
	"log"
	"math"
	"net/http"
	"net/url"
	"time"
)

var withingsOauthConfig = &oauth2.Config{
	RedirectURL:  "http://localhost:8080/callbackWithings",
	ClientID:     "fdb372f760c5543c30ded5b770af67d904b376559edabe2521bb74e7bd5e02e7",
	ClientSecret: "8498abe0a56dc901ccdb6e5c5dbe5bb80185ec865a74e2718bf547b26f12a9c1",
	Scopes:       []string{"user.metrics"}, // Set the required scopes
	Endpoint: oauth2.Endpoint{
		AuthURL:  "https://account.withings.com/oauth2_user/authorize2",
		TokenURL: "https://wbsapi.withings.net/v2/oauth2",
	},
}

func handleWithingsLogin(c *gin.Context) {
	stateToken, err := generateStateToken()
	if err != nil {
		log.Printf("Error generating state token: %s\n", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		log.Printf("Error getting session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	session.Values["oauth_state"] = stateToken
	err = session.Save(c.Request, c.Writer)
	if err != nil {
		log.Printf("Error saving session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	url := withingsOauthConfig.AuthCodeURL(stateToken, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func handleWithingsCallback(c *gin.Context) {
	code := c.Query("code")

	token, err := exchangeTokenWithWithings(code)
	if err != nil {
		log.Printf("Could not get access token: %s\n", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	log.Printf("Received Withings tokens, Access: %s, Refresh: %s", token.AccessToken, token.RefreshToken)

	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		log.Printf("Error getting session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	session.Values["withings_access_token"] = token.AccessToken
	session.Values["withings_refresh_token"] = token.RefreshToken

	if err = session.Save(c.Request, c.Writer); err != nil {
		log.Printf("Error saving session: %s", err)
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	log.Println("Session saved with Withings tokens")

	c.Redirect(http.StatusFound, "/")
}

func exchangeTokenWithWithings(code string) (*oauth2.Token, error) {
	log.Printf("Exchanging Withings code: %s", code)

	postData := url.Values{}
	postData.Set("action", "requesttoken")
	postData.Set("grant_type", "authorization_code")
	postData.Set("client_id", withingsOauthConfig.ClientID)
	postData.Set("client_secret", withingsOauthConfig.ClientSecret)
	postData.Set("code", code)
	postData.Set("redirect_uri", withingsOauthConfig.RedirectURL)

	log.Printf("Token exchange request data: %v", postData)

	resp, err := http.PostForm(withingsOauthConfig.Endpoint.TokenURL, postData)
	if err != nil {
		log.Printf("Error in token exchange request: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	var withingsResponse WithingsTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&withingsResponse); err != nil {
		log.Printf("Error decoding token exchange response: %v", err)
		return nil, err
	}

	log.Printf("Received Withings tokens, Access: %s, Refresh: %s", withingsResponse.Body.AccessToken, withingsResponse.Body.RefreshToken)

	token := &oauth2.Token{
		AccessToken:  withingsResponse.Body.AccessToken,
		RefreshToken: withingsResponse.Body.RefreshToken,
		TokenType:    withingsResponse.Body.TokenType,
		Expiry:       time.Now().Add(time.Second * time.Duration(withingsResponse.Body.ExpiresIn)),
	}

	return token, nil
}

func refreshWithingsToken(refreshToken string) (*oauth2.Token, error) {
	postData := url.Values{}
	postData.Set("grant_type", "withings_refresh_token")
	postData.Set("client_id", withingsOauthConfig.ClientID)
	postData.Set("client_secret", withingsOauthConfig.ClientSecret)
	postData.Set("withings_refresh_token", refreshToken)

	resp, err := http.PostForm(withingsOauthConfig.Endpoint.TokenURL, postData)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var newToken oauth2.Token
	if err := json.NewDecoder(resp.Body).Decode(&newToken); err != nil {
		return nil, err
	}

	return &newToken, nil
}

func generateStateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func fetchWithingsData(c *gin.Context, date time.Time) (*WithingsAPIResponse, error) {
	session := sessions.Default(c)

	refreshToken, ok := session.Get("withings_refresh_token").(string)
	if !ok || refreshToken == "" {
		log.Println("Refresh token not found in session.")
		return nil, errors.New("refresh token not found")
	}

	accessToken, ok := session.Get("withings_access_token").(string)
	if !ok || accessToken == "" {
		log.Println("Access token not found in session.")
		return nil, errors.New("access token not found")
	}

	client := withingsOauthConfig.Client(context.Background(), &oauth2.Token{AccessToken: accessToken})

	startTimestamp := date.Unix()
	endTimestamp := date.AddDate(0, 0, 1).Unix()
	reqURL := fmt.Sprintf("https://wbsapi.withings.net/measure?action=getmeas&meastype=1&category=1&startdate=%d&enddate=%d", startTimestamp, endTimestamp)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

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

func convertToPounds(value int, unit int) float64 {
	kilograms := float64(value) / math.Pow(10, float64(-unit))
	pounds := math.Round((kilograms*2.20462)*100) / 100
	return pounds
}

type WithingsAPIResponse struct {
	Status int `json:"status"`
	Body   struct {
		UpdateTime  int64  `json:"updatetime"`
		Timezone    string `json:"timezone"`
		MeasureGrps []struct {
			GrpID    int64  `json:"grpid"`
			Attrib   int    `json:"attrib"`
			Date     int64  `json:"date"`
			Created  int64  `json:"created"`
			Modified int64  `json:"modified"`
			Category int    `json:"category"`
			DeviceID string `json:"deviceid"`
			Measures []struct {
				Value int `json:"value"`
				Type  int `json:"type"`
				Unit  int `json:"unit"`
				Algo  int `json:"algo"`
				FM    int `json:"fm"`
			} `json:"measures"`
			ModelID int         `json:"modelid"`
			Model   string      `json:"model"`
			Comment interface{} `json:"comment"`
		} `json:"measuregrps"`
	} `json:"body"`
}

type WithingsTokenResponse struct {
	Status int `json:"status"`
	Body   struct {
		UserID       string `json:"userid"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	} `json:"body"`
}
