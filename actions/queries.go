/*
 * +===============================================
 * | Author:        Parham Alvani <parham.alvani@gmail.com>
 * |
 * | Creation Date: 09-02-2018
 * |
 * | File Name:     main.go
 * +===============================================
 */

package actions

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/configor"
	log "github.com/sirupsen/logrus"
)

// Config represents main configuration
var Config = struct {
	DB struct {
		URL string `default:"127.0.0.1" env:"db_url"`
	}
}{}

// ISRC database
var isrcDB *mgo.Database

func init() {
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = log.WithFields(log.Fields{
		"component": "dm",
	}).Writer()
}

// handle registers apis and create http handler
func handle() http.Handler {
	r := gin.Default()

	api := r.Group("/api")
	{
		api.GET("/about", aboutHandler)

		api.GET("/things", thingsHandler)
		api.GET("/things/:thingid", thingDataHandler)
		api.GET("/things/:thingid/p", thingLastParsedHandler)
		api.POST("/things/w", thingsDataHandlerWindowing)
		api.POST("/things", thingsDataHandler)

	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "404 Not Found"})
	})

	return r
}

func setupDB() {
	// Create a Mongo Session
	session, err := mgo.Dial(Config.DB.URL)
	if err != nil {
		log.Fatalf("Mongo session %s: %v", Config.DB.URL, err)
	}
	isrcDB = session.DB("isrc")

	// gateway collection
	cg := session.DB("isrc").C("gateway")
	if err := cg.EnsureIndex(mgo.Index{
		Key:         []string{"timestamp"},
		ExpireAfter: 120 * time.Second,
	}); err != nil {
		panic(err)
	}

	// Optional. Switch the session to a monotonic behavior.
	session.SetMode(mgo.Monotonic, true)
}

func main() {
	// Load configuration
	if err := configor.Load(&Config, "config.yml"); err != nil {
		panic(err)
	}

	setupDB()

	fmt.Println("DM AIoTRC @ 2018")

	srv := &http.Server{
		Addr:    ":1372",
		Handler: handle(),
	}

	go func() {
		fmt.Printf("DM Listen: %s\n", srv.Addr)
		// service connections
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal("Listen Error:", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
	fmt.Println("DM Shutdown")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Shutdown Error:", err)
	}
}

func aboutHandler(c *gin.Context) {
	c.String(http.StatusOK, "18.20 is leaving us")
}

func thingsHandler(c *gin.Context) {
	var results []bson.M

	pipe := isrcDB.C("data").Pipe([]bson.M{
		{"$group": bson.M{"_id": "$thingid", "total": bson.M{"$sum": 1}}},
	})
	if err := pipe.All(&results); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, results)
}

func thingsDataHandlerWindowing(c *gin.Context) {
	var results []bson.M

	var json struct {
		ThingIDs      []string `json:"thing_ids"`
		Since         int64
		Until         int64
		ClusterNumber int64 `json:"cn"`
	}
	if err := c.BindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if json.ClusterNumber == 0 {
		json.ClusterNumber = 200
	}

	// cluster size
	cs := (json.Until - json.Since) / json.ClusterNumber
	cs *= 1000
	if cs == 0 {
		cs++
	}

	pipe := isrcDB.C("data").Pipe([]bson.M{
		{"$match": bson.M{
			"thingid": bson.M{
				"$in": json.ThingIDs,
			},
			"data": bson.M{
				"$ne": nil,
			},
			"timestamp": bson.M{
				"$gt": time.Unix(json.Since, 0),
				"$lt": time.Unix(json.Until, 0),
			},
		}},
		{"$group": bson.M{
			"_id": bson.M{
				"thingid": "$thingid",
				"cluster": bson.M{"$floor": bson.M{"$divide": []interface{}{
					bson.M{
						"$subtract": []interface{}{
							"$timestamp",
							time.Unix(0, 0),
						},
					},
					cs,
				}}},
			},
			"count": bson.M{"$sum": 1},
			// "data":  bson.M{"$push": bson.M{"$cond": []interface{}{bson.M{"$ne": []interface{}{"$data", nil}}, "$data", "$noval"}}},
			"data": bson.M{"$last": "$data"},
		}},
		{"$addFields": bson.M{
			"since": bson.M{"$add": []interface{}{time.Unix(0, 0), bson.M{"$multiply": []interface{}{"$_id.cluster", cs}}}},
			"until": bson.M{"$add": []interface{}{time.Unix(0, 0), cs, bson.M{"$multiply": []interface{}{"$_id.cluster", cs}}}},
		}},
		{"$sort": bson.M{"since": -1}},
	})
	if err := pipe.All(&results); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, results)

}

func thingDataHandler(c *gin.Context) {
	var results []bson.M

	id := c.Param("thingid")

	since, err := strconv.ParseInt(c.Query("since"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	until, err := strconv.ParseInt(c.Query("until"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pipe := isrcDB.C("data").Pipe([]bson.M{
		{"$match": bson.M{
			"thingid": id,
			"timestamp": bson.M{
				"$gt": time.Unix(since, 0),
				"$lt": time.Unix(until, 0),
			},
		}},
		{"$sort": bson.M{"timestamp": -1}},
	})
	if err := pipe.All(&results); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, results)
}

func thingLastParsedHandler(c *gin.Context) {
	var results []bson.M

	id := c.Param("thingid")

	pipe := isrcDB.C("data").Pipe([]bson.M{
		{"$match": bson.M{
			"thingid": id,
			"data":    bson.M{"$ne": nil},
		}},
		{"$project": bson.M{"timestamp": true}},
		{"$sort": bson.M{"timestamp": -1}},
		{"$limit": 1},
	})
	if err := pipe.All(&results); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(results) > 0 {
		c.JSON(http.StatusOK, results[0]["timestamp"])
	} else {
		c.JSON(http.StatusOK, "0")
	}
}

func thingsDataHandler(c *gin.Context) {
	var results []bson.M

	var json struct {
		ThingIDs []string `json:"thing_ids"`
		Since    int64
		Until    int64
		Limit    int64
		Offset   int64
	}

	if err := c.BindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if json.Until == 0 {
		json.Until = time.Now().Unix()
	}

	if json.Limit == 0 {
		json.Limit = math.MaxInt64
	}

	if len(json.ThingIDs) > 0 {
		pipe := isrcDB.C("data").Pipe([]bson.M{
			{"$match": bson.M{
				"thingid": bson.M{
					"$in": json.ThingIDs,
				},
				"data": bson.M{
					"$ne": nil,
				},
				"timestamp": bson.M{
					"$gt": time.Unix(json.Since, 0),
					"$lt": time.Unix(json.Until, 0),
				},
			}},
			{"$skip": json.Offset},
			{"$limit": json.Limit},
			{"$sort": bson.M{"timestamp": -1}},
		})
		if err := pipe.All(&results); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		pipe := isrcDB.C("data").Pipe([]bson.M{
			{"$match": bson.M{
				"data": bson.M{
					"$ne": nil,
				},
				"timestamp": bson.M{
					"$gt": time.Unix(json.Since, 0),
					"$lt": time.Unix(json.Until, 0),
				},
			}},
			{"$skip": json.Offset},
			{"$limit": json.Limit},
			{"$sort": bson.M{"timestamp": -1}},
		})
		if err := pipe.All(&results); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

	}

	c.JSON(http.StatusOK, results)

}