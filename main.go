package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/labstack/echo"
	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
	"gopkg.in/jmcvetta/napping.v3"
	"gopkg.in/redis.v5"
)

type Settings struct {
	Host               string `envconfig:"HOST" required:"true"`
	Port               string `envconfig:"PORT" required:"true"`
	RedisAddr          string `envconfig:"REDIS_ADDR" required:"true"`
	RedisPassword      string `envconfig:"REDIS_PASSWORD" required:"true"`
	GitHubClientId     string `envconfig:"GITHUB_CLIENT_ID" required:"true"`
	GitHubClientSecret string `envconfig:"GITHUB_CLIENT_SECRET" required:"true"`
}

var err error
var s Settings
var rds *redis.Client

func main() {
	err = envconfig.Process("", &s)
	if err != nil {
		log.Fatal("couldn't process envconfig: ", err)
	}

	// redis
	rds = redis.NewClient(&redis.Options{
		Addr:     s.RedisAddr,
		Password: s.RedisPassword,
	})

	e := echo.New()

	e.GET("/", index)
	e.GET("/_authorize", authorize)
	e.GET("/_callback", authorizeCallback)
	e.GET("/:user/:repo", drawChart)

	log.Fatal(e.Start(":" + os.Getenv("PORT")))
}

func index(c echo.Context) error {
	return c.Redirect(302, "https://github.com/fiatjaf/github-traffic")
}

func authorize(c echo.Context) error {
	return c.Redirect(302,
		"https://github.com/login/oauth/authorize"+
			"?client_id="+s.GitHubClientId+
			"&redirect_uri="+s.Host+"/_callback"+
			"&scope=public_repo",
	)
}

func authorizeCallback(c echo.Context) error {
	code := c.QueryParam("code")

	res := struct {
		AccessToken string `json:"access_token"`
	}{}

	headers := &http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	if _, err := napping.Send(&napping.Request{
		Url:    "https://github.com/login/oauth/access_token",
		Method: "POST",
		Payload: map[string]string{
			"code":          code,
			"client_id":     s.GitHubClientId,
			"client_secret": s.GitHubClientSecret,
			"redirect_uri":  s.Host + "/_callback",
		},
		Header: headers,
		Result: &res,
	}); err != nil {
		log.Print(err)
		return err
	} else if res.AccessToken == "" {
		err = fmt.Errorf("failed to fetch access token from github.")
		log.Print(err)
		return err
	}

	user := struct {
		Login string `json:"login"`
	}{}

	headers.Set("Accept", "application/vnd.github.v3+json")
	headers.Set("User-Agent", "https://github.com/fiatjaf/github-traffic")
	headers.Set("Authorization", "token "+res.AccessToken)
	if _, err := napping.Send(&napping.Request{
		Url:    "https://api.github.com/user",
		Method: "GET",
		Header: headers,
		Result: &user,
	}); err != nil {
		log.Print(err)
		return err
	} else if user.Login == "" {
		err = fmt.Errorf("failed to fetch user login from github after oauth.")
		log.Print(err)
		return err
	}

	if err = rds.Set("token:"+user.Login, res.AccessToken, 0).Err(); err != nil {
		log.Print(err)
		return err
	}

	return c.String(200, "done.")
}

type GitHubStats struct {
	Count   int `json:"count"`
	Uniques int `json:"uniques"`
	Views   []struct {
		Count     int    `json:"count"`
		Timestamp string `json:"timestamp"`
		Uniques   int    `json:"uniques"`
	} `json:"views"`
}

func drawChart(c echo.Context) error {
	// get token for this user
	authUser := c.QueryParam("user") // because a user name may be used to authorize others' repos
	if authUser == "" {
		authUser = c.Param("user") // the default
	}

	token, err := rds.Get("token:" + authUser).Result()
	if err != nil {
		return echo.NewHTTPError(404, "user doesn't have a valid GitHub token registered.")
	}

	repo := c.Param("user") + "/" + c.Param("repo")
	stats := GitHubStats{}
	log.Print("~ view: " + repo)

	// try to fetch cached data from redis
	rediskey := "stats:" + repo
	cached, err := rds.Get(rediskey).Bytes()
	if err == nil && cached != nil && len(cached) > 0 {
		if err := json.Unmarshal(cached, &stats); err != nil {
			log.Print("data at cache is invalid: ", string(cached), " // ", err)
		} else {
			log.Print("cache hit")
		}
	} else {
		// get data from github
		headers := &http.Header{}
		headers.Set("User-Agent", "https://github.com/fiatjaf/github-traffic")
		headers.Set("Accept", "application/vnd.github.v3+json")
		headers.Set("Authorization", "token "+token)
		if _, err = napping.Send(&napping.Request{
			Url:    "https://api.github.com/repos/" + repo + "/traffic/views",
			Method: "GET",
			Header: headers,
			Result: &stats,
		}); err != nil {
			return err
		} else if len(stats.Views) == 0 {
			log.Print("no data received from GitHub.")
			return echo.NewHTTPError(502, "no data received from GitHub.")
		}

		// cache results on redis
		cache, _ := json.Marshal(stats)
		if err = rds.Set(rediskey, cache, time.Hour*2).Err(); err != nil {
			log.Print("failed to cache results on redis: ", err)
		}
	}

	// build chart
	uniqueSessions := chart.TimeSeries{
		Name: "Unique visitors",
		Style: chart.Style{
			Show:        true,
			StrokeWidth: 3.2,
			StrokeColor: drawing.Color{52, 116, 219, 100},
			FillColor:   drawing.Color{52, 116, 219, 65},
		},
		XValues: make([]time.Time, len(stats.Views)),
		YValues: make([]float64, len(stats.Views)),
	}
	totalPageviews := chart.TimeSeries{
		Name: "Views",
		Style: chart.Style{
			Show:        true,
			StrokeWidth: 3.1,
			StrokeColor: drawing.Color{21, 198, 148, 100},
		},
		XValues: make([]time.Time, len(stats.Views)),
		YValues: make([]float64, len(stats.Views)),
	}
	for i, stat := range stats.Views {
		date, _ := time.Parse("2006-01-02T15:04:05Z", stat.Timestamp)
		uniqueSessions.XValues[i] = date
		totalPageviews.XValues[i] = date
		uniqueSessions.YValues[i] = float64(stat.Uniques)
		totalPageviews.YValues[i] = float64(stat.Count)
	}

	w := 444
	h := 200
	graph := chart.Chart{
		Width:  w,
		Height: h,
		Series: []chart.Series{uniqueSessions, totalPageviews},
		Background: chart.Style{
			Padding: chart.Box{Top: 10, Right: 10, Bottom: 10},
		},
		XAxis: chart.XAxis{
			Style: chart.Style{Show: true},
			ValueFormatter: func(v interface{}) string {
				return time.Unix(0, int64(v.(float64))).Format("Jan 02")
			},
		},
		YAxis: chart.YAxis{
			Style:          chart.Style{Show: true},
			ValueFormatter: func(v interface{}) string { return strconv.Itoa(int(v.(float64))) },
		},
	}

	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	c.Response().Header().Set("Content-Type", "image/png")
	graph.Render(chart.PNG, c.Response())
	return nil
}
