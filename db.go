package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	//"go.mongodb.org/mongo-driver/mongo/readpref"
)

var dbName = "sarpedon"
var cachedImageData = make(map[string]scoreEntry)

type teamData struct {
	Team       TeamData
	ImageCount int
	Score      int
	Time       string
}

type scoreEntry struct {
	Time           time.Time     `json:"time,omitempty"`
	Team           string        `json:"team,omitempty"`
	Image          string        `json:"image,omitempty"`
	Vulns          vulnWrapper    `json:"vulns,omitempty"`
	Points         int           `json:"points,omitempty"`
	PlayTime       time.Duration `json:"playtime,omitempty"`
	PlayTimeStr    string        `json:"playtimestr,omitempty"`
	ElapsedTime    time.Duration `json:"elapsedtime,omitempty"`
	ElapsedTimeStr string        `json:"playtimestr,omitempty"`
}


type vulnWrapper struct {
	VulnsScored int `json:"vulnsscored,omitempty"`
	VulnsTotal int `json:"vulnstotal,omitempty"`
	VulnItems []vulnItem `json:"vulnitems,omitempty"`
}
type vulnItem struct {
	VulnText   string `json:"vulntext,omitempty"`
	VulnPoints int    `json:"vulnpoints,omitempty"`
}

func initDatabase() (*mongo.Client, context.Context) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal(err)
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	err = client.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}
	return client, ctx
}

func getAll(teamName, imageName string) []scoreEntry {
	client, ctx := initDatabase()
	defer client.Disconnect(ctx)

	scores := []scoreEntry{}
	coll := client.Database(dbName).Collection("results")
	teamObj := getTeam(teamName)
	findOptions := options.Find()
	findOptions.SetSort(bson.D{{"time", 1}})
	var cursor *mongo.Cursor
	var err error
	if imageName != "" {
		fmt.Println("image specificed, searching for all records ")
		cursor, err = coll.Find(context.TODO(), bson.D{{"team", teamObj.Id}, {"image", imageName}}, findOptions)
		if err != nil {
			panic(err)
		}
	} else {
		fmt.Println("no imag, seaaaaarrchchhin", teamObj.Id, imageName)
		cursor, err = coll.Find(context.TODO(), bson.D{{"team", teamObj.Id}}, findOptions)
		if err != nil {
			panic(err)
		}
	}
	if err := cursor.All(ctx, &scores); err != nil {
		panic(err)
	}
	fmt.Println("all score results", scores)
	return scores
}

func getScores() ([]scoreEntry, error) {
	client, ctx := initDatabase()
	defer client.Disconnect(ctx)

	scores := []scoreEntry{}
	coll := client.Database(dbName).Collection("results")

	groupStage := bson.D{
		{"$group", bson.D{
			{"_id", bson.D{
				{"image", "$image"},
				{"team", "$team"},
			}},
			{"time", bson.D{
				{"$min", "$time"},
			}},
			{"points", bson.D{
				{"$last", "$points"},
			}},
			{"playtime", bson.D{
				{"$last", "$playtime"},
			}},
			{"elapsedtime", bson.D{
				{"$last", "$elapsedtime"},
			}},
			{"vulns", bson.D{
				{"$last", "$vulns"},
			}},
		}},
	}

	projectStage := bson.D{
		{"$project", bson.D{
			{"time", "$time"},
			{"team", "$_id.team"},
			{"image", "$_id.image"},
			{"points", "$points"},
			{"playtime", "$playtime"},
			{"elapsedtime", "$elapsedtime"},
			{"vulns", "$vulns"},
		}},
	}

	opts := options.Aggregate().SetMaxTime(2 * time.Second)

	cursor, err := coll.Aggregate(context.TODO(), mongo.Pipeline{groupStage, projectStage}, opts)

	if err != nil {
		return scores, err
	}

	if err = cursor.All(context.TODO(), &scores); err != nil {
		return scores, err
	}

	return scores, nil
}


func getCsv() string {
	teamScores, err := getScores()
	if err != nil {
		panic(err)
	}
	csvString := "Email,Alias,Team Id,Image,Score,Play Time,Elapsed Time\n"
	for _, score := range teamScores {
		teamObj := getTeam(score.Team)
		csvString += teamObj.Email + ","
		csvString += teamObj.Alias + ","
		csvString += teamObj.Id + ","
		csvString += score.Image + ","
		csvString += fmt.Sprintf("%d,", score.Points)
		csvString += formatTime(score.PlayTime) + ","
		csvString += formatTime(score.ElapsedTime) + "\n"
	}
	return csvString
}

func getScore(teamName, imageName string) []scoreEntry {
	scoreResults := []scoreEntry{}
	teamObj := getTeam(teamName)
	if imageName != "" {
		if data, ok := cachedImageData[teamObj.Id+"@"+imageName]; ok {
			scoreResults = append(scoreResults, data)
		} else {
			fmt.Println("fetching new score for", teamObj.Id, imageName)
			teamScores, err := getScores()
			if err != nil {
				panic(err)
			}
			for _, score := range teamScores {
				if score.Image == imageName && score.Team == teamObj.Id {
					fmt.Println("found it bro", score)
					scoreResults = append(scoreResults, score)
				}
			}
		}
	} else {
		for _, image := range sarpConfig.Image {
			if data, ok := cachedImageData[teamObj.Id+"@"+image.Name]; ok {
				fmt.Println("found cached image data for", teamName, data)
				scoreResults = append(scoreResults, data)
			} else {
				fmt.Println("fetching new score for", teamObj.Id, image.Name)
				teamScores, err := getScores()
				if err != nil {
					panic(err)
				}
				for _, score := range teamScores {
					if score.Image == image.Name && score.Team == teamObj.Id {
						fmt.Println("found it bro", score)
						scoreResults = append(scoreResults, score)
					}
				}
			}
		}
	}

	for index, result := range scoreResults {
		scoreResults[index].PlayTimeStr = formatTime(result.PlayTime)
		scoreResults[index].ElapsedTimeStr = formatTime(result.ElapsedTime)
	}
	return scoreResults
}

func parseScoresIntoTeam(scores []scoreEntry) (teamData, error) {
	data, err := parseScoresIntoTeams(scores)
	if err != nil || len(data) <= 0 {
		return teamData{}, err
	}
	return data[0], nil
}

func parseScoresIntoTeams(scores []scoreEntry) ([]teamData, error) {
	td := []teamData{}
	if len(scores) <= 0 {
		return td, nil
	}

	imageCount := 0
	totalScore := 0
	playTime, _ := time.ParseDuration("0s")
	currentTeam := scores[0].Team

	for _, score := range scores {
		if currentTeam != score.Team {
			td = append(td, teamData{
				Team:       getTeam(currentTeam),
				ImageCount: imageCount,
				Score:      totalScore,
				Time:       formatTime(playTime),
			})
			imageCount = 0
			totalScore = 0
			playTime, _ = time.ParseDuration("0s")
			currentTeam = score.Team
		}
		imageCount += 1
		totalScore += score.Points
		playTime += score.PlayTime
	}

	td = append(td, teamData{
		Team:       getTeam(scores[len(scores)-1].Team),
		ImageCount: imageCount,
		Score:      totalScore,
		Time:       formatTime(playTime),
	})

	sort.SliceStable(td, func(i, j int) bool {
		var result bool
		if td[i].Score == td[j].Score {
			result = td[i].Time < td[j].Time
		} else {
			result = td[i].Score > td[j].Score
		}
		return result
	})

	return td, nil
}

func insertScore(newEntry scoreEntry) error {
	client, ctx := initDatabase()
	defer client.Disconnect(ctx)
	collection := client.Database(dbName).Collection("results")
	insertedScore, err := collection.InsertOne(context.TODO(), newEntry)
	cachedImageData[newEntry.Team+"@"+newEntry.Image] = newEntry
	if err != nil {
		return err
	}
	fmt.Println("inserted with id", insertedScore.InsertedID)
	return nil
}

func getLastScore(newEntry *scoreEntry) (scoreEntry, error) {
	// Cached image data stored in format teamId@image
	if data, ok := cachedImageData[newEntry.Team+"@"+newEntry.Image]; ok {
		fmt.Println("using cached", data)
		fmt.Println("comapred to newentry", newEntry)
		return data, nil
	} else {
		scores, err := getScores()
		if err == nil {
			for _, score := range scores {
				if score.Team == newEntry.Team && score.Image == newEntry.Image {
					fmt.Println("found score", score)
					fmt.Println("comapred to newentry", newEntry)
					return score, nil
				}
			}
		}
	}
	return scoreEntry{}, errors.New("Couldn't find last image record")
}

func calcPlayTime(newEntry *scoreEntry) error {
	threshhold, _ := time.ParseDuration("5m")
	recentRecord, err := getLastScore(newEntry)
	var timeDifference time.Duration
	if err != nil {
		fmt.Println("playtime: no previous record! time is 0")
		timeDifference, _ = time.ParseDuration("0s")
	} else {
		timeDifference = newEntry.Time.Sub(recentRecord.Time)
		fmt.Println("playtime: time diff is", timeDifference)
	}
	if timeDifference < threshhold {
		fmt.Println("Adding timediff for playtime", timeDifference)
		newEntry.PlayTime = recentRecord.PlayTime + timeDifference
	} else {
		newEntry.PlayTime = recentRecord.PlayTime
	}
	return nil
}

func calcElapsedTime(newEntry *scoreEntry) error {
	recentRecord, err := getLastScore(newEntry)
	var timeDifference time.Duration
	if err != nil {
		fmt.Println("elaptime: no previous record! time is 0")
		timeDifference, _ = time.ParseDuration("0s")
	} else {
		timeDifference = newEntry.Time.Sub(recentRecord.Time)
		fmt.Println("elaptime: time diff is", timeDifference)
	}
	fmt.Println("Adding timediff for elaptime", timeDifference)
	newEntry.ElapsedTime = recentRecord.ElapsedTime + timeDifference
	fmt.Println("Elaptime is now", newEntry.ElapsedTime)
	return nil
}

func formatTime(dur time.Duration) string {
	durSeconds := dur.Microseconds() / 1000000
	fmt.Println("=======")
	fmt.Println("durnum", durSeconds)
	seconds := durSeconds % 60
	fmt.Println("seconds", seconds)
	durSeconds -= seconds
	fmt.Println("durnum", durSeconds)
	minutes := (durSeconds % (60 * 60)) / 60
	fmt.Println("minutes", minutes)
	durSeconds -= minutes * 60
	fmt.Println("durnum", durSeconds)
	hours := durSeconds / (60 * 60)
	fmt.Println("hours", hours)
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)

}